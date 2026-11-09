package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultUserAgent   = "badgeboard (+https://github.com/NotRealShanks/badgeboard)"
	DefaultTimeout     = 15 * time.Second
	DefaultMaxRetries  = 3
	DefaultBaseBackoff = 250 * time.Millisecond
	DefaultMaxBackoff  = 10 * time.Second
)

// Typed errors supporting errors.Is and errors.As
var (
	// ErrNotFoundOrPrivate indicates HTTP 404 or a redirect to a sign-in / login page.
	ErrNotFoundOrPrivate = errors.New("profile not found or private")
	// ErrRateLimited indicates persistent HTTP 429 after all retries were exhausted.
	ErrRateLimited = errors.New("rate limit exceeded (HTTP 429)")
	// ErrNetwork indicates a transport error, timeout, or exhausted 5xx server failure.
	ErrNetwork = errors.New("network or server communication failure")
)

// HTTPClient represents the interface needed to execute HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Fetcher retrieves profile HTML pages over HTTP with rate limiting, retries, and redirect handling.
type Fetcher struct {
	client      HTTPClient
	userAgent   string
	maxRetries  int
	baseBackoff time.Duration
	maxBackoff  time.Duration
	sleeper     func(ctx context.Context, d time.Duration) error
}

// Option allows configuring Fetcher instances.
type Option func(*Fetcher)

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(client HTTPClient) Option {
	return func(f *Fetcher) {
		if client != nil {
			f.client = client
		}
	}
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(f *Fetcher) {
		if ua != "" {
			f.userAgent = ua
		}
	}
}

// WithMaxRetries sets the maximum number of retries.
func WithMaxRetries(retries int) Option {
	return func(f *Fetcher) {
		if retries >= 0 {
			f.maxRetries = retries
		}
	}
}

// WithBackoff sets base and max backoff for exponential backoff.
func WithBackoff(base, max time.Duration) Option {
	return func(f *Fetcher) {
		if base > 0 {
			f.baseBackoff = base
		}
		if max > 0 {
			f.maxBackoff = max
		}
	}
}

// WithSleeper sets a custom context-aware sleep function for testing.
func WithSleeper(sleeper func(ctx context.Context, d time.Duration) error) Option {
	return func(f *Fetcher) {
		if sleeper != nil {
			f.sleeper = sleeper
		}
	}
}

// defaultContextSleep sleeps for duration d or aborts if ctx is canceled.
func defaultContextSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// New creates a new Fetcher with honest User-Agent, redirects handling, and defaults.
func New(opts ...Option) *Fetcher {
	// Standard client with redirect inspection
	stdClient := &http.Client{
		Timeout: DefaultTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			lowerPath := strings.ToLower(req.URL.Path)
			if strings.Contains(lowerPath, "sign_in") || strings.Contains(lowerPath, "login") {
				// Redirecting to login/sign-in page means profile is private or not found
				return ErrNotFoundOrPrivate
			}
			return nil
		},
	}

	f := &Fetcher{
		client:      stdClient,
		userAgent:   DefaultUserAgent,
		maxRetries:  DefaultMaxRetries,
		baseBackoff: DefaultBaseBackoff,
		maxBackoff:  DefaultMaxBackoff,
		sleeper:     defaultContextSleep,
	}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

// Fetch loads the HTML content for the specified profile URL.
// It applies context timeouts, exponential backoff with jitter on 429/5xx,
// and maps sign-in redirects and 404s directly to ErrNotFoundOrPrivate.
func (f *Fetcher) Fetch(ctx context.Context, targetURL string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}

	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", targetURL, err)
	}

	var lastErr error
	var wasRateLimited bool

	for attempt := 0; attempt <= f.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := f.calculateBackoff(attempt, lastErr)
			if sleepErr := f.sleeper(ctx, backoff); sleepErr != nil {
				return nil, fmt.Errorf("%w: %v", ErrNetwork, sleepErr)
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("User-Agent", f.userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml")

		resp, err := f.client.Do(req)
		if err != nil {
			// Check if redirect stopped at sign-in
			if errors.Is(err, ErrNotFoundOrPrivate) {
				return nil, ErrNotFoundOrPrivate
			}
			// If context was canceled
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
			}
			lastErr = err
			continue
		}

		// Inspect final URL: if redirected to sign-in page without triggering CheckRedirect
		finalPath := strings.ToLower(resp.Request.URL.Path)
		if strings.Contains(finalPath, "sign_in") || strings.Contains(finalPath, "login") {
			_ = resp.Body.Close()
			return nil, ErrNotFoundOrPrivate
		}

		// Handle HTTP status codes
		switch {
		case resp.StatusCode == http.StatusOK:
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				lastErr = fmt.Errorf("reading response body: %w", readErr)
				continue
			}
			return body, nil

		case resp.StatusCode == http.StatusNotFound:
			_ = resp.Body.Close()
			return nil, ErrNotFoundOrPrivate

		case resp.StatusCode == http.StatusTooManyRequests:
			wasRateLimited = true
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("HTTP 429 Too Many Requests (retry-after: %v)", retryAfter)
			if retryAfter > 0 {
				// Sleep specified retry-after duration on 429
				if sleepErr := f.sleeper(ctx, retryAfter); sleepErr != nil {
					return nil, fmt.Errorf("%w: %v", ErrNetwork, sleepErr)
				}
			}
			continue

		case resp.StatusCode >= 500 && resp.StatusCode <= 599:
			wasRateLimited = false
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("server error (HTTP %d)", resp.StatusCode)
			if retryAfter > 0 {
				if sleepErr := f.sleeper(ctx, retryAfter); sleepErr != nil {
					return nil, fmt.Errorf("%w: %v", ErrNetwork, sleepErr)
				}
			}
			continue

		default:
			_ = resp.Body.Close()
			return nil, fmt.Errorf("%w: unexpected HTTP status %d", ErrNetwork, resp.StatusCode)
		}
	}

	if wasRateLimited {
		return nil, fmt.Errorf("%w: %v", ErrRateLimited, lastErr)
	}
	return nil, fmt.Errorf("%w: retries exhausted: %v", ErrNetwork, lastErr)
}

func (f *Fetcher) calculateBackoff(attempt int, _ error) time.Duration {
	// Exponential backoff: base * 2^(attempt-1) + jitter
	multiplier := 1 << (attempt - 1)
	backoff := f.baseBackoff * time.Duration(multiplier)
	if backoff > f.maxBackoff {
		backoff = f.maxBackoff
	}

	// Full jitter between [backoff/2, backoff]
	half := int64(backoff / 2)
	if half > 0 {
		jitter := time.Duration(rand.Int63n(half))
		backoff = time.Duration(half) + jitter
	}

	return backoff
}

func parseRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}

	// Format A: delta-seconds (e.g. "5" or "120")
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	// Format B: HTTP-date (e.g. "Wed, 21 Oct 2026 07:28:00 GMT")
	if targetTime, err := http.ParseTime(header); err == nil {
		delta := time.Until(targetTime)
		if delta > 0 {
			return delta
		}
	}

	return 0
}
