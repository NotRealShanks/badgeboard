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
	DefaultUserAgent       = "badgeboard (+https://github.com/NotRealShanks/badgeboard)"
	DefaultTimeout         = 15 * time.Second
	DefaultMaxRetries      = 3
	DefaultBaseBackoff     = 250 * time.Millisecond
	DefaultMaxBackoff      = 10 * time.Second
	DefaultMaxResponseSize = 2 * 1024 * 1024 // 2 MB
	DefaultMaxRetryAfter   = 60 * time.Second
	MaxRedirectHops        = 5
)

// Allowed redirect hosts
var defaultAllowedHosts = map[string]bool{
	"www.skills.google":       true,
	"skills.google":           true,
	"cloudskillsboost.google": true,
}

// Typed errors supporting errors.Is and errors.As
var (
	// ErrNotFoundOrPrivate indicates HTTP 404 or a redirect to a sign-in / login page.
	ErrNotFoundOrPrivate = errors.New("profile not found or private")
	// ErrRateLimited indicates persistent HTTP 429 after retries, or Retry-After exceeding max clamp.
	ErrRateLimited = errors.New("rate limit exceeded (HTTP 429)")
	// ErrNetwork indicates a transport error, timeout, or exhausted 5xx server failure.
	ErrNetwork = errors.New("network or server communication failure")
	// ErrTooManyRedirects indicates redirect hops exceeded MaxRedirectHops (5).
	ErrTooManyRedirects = errors.New("too many redirects (exceeded 5 hops)")
	// ErrUntrustedRedirect indicates redirect target host is not an allowed skills.google domain.
	ErrUntrustedRedirect = errors.New("redirect to untrusted host refused")
	// ErrResponseTooLarge indicates the server response body exceeded the 2MB limit.
	ErrResponseTooLarge = errors.New("response body exceeds 2MB limit")
)

// Limiter provides shared rate limiting and global cool-down pauses.
type Limiter interface {
	Wait(ctx context.Context) error
	PauseFor(d time.Duration)
}

// HTTPClient represents the interface needed to execute HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Fetcher retrieves profile HTML pages over HTTP with rate limiting, retries, and redirect handling.
type Fetcher struct {
	client           HTTPClient
	userAgent        string
	maxRetries       int
	baseBackoff      time.Duration
	maxBackoff       time.Duration
	maxResponseBytes int64
	maxRetryAfter    time.Duration
	limiter          Limiter
	allowedHosts     map[string]bool
	sleeper          func(ctx context.Context, d time.Duration) error
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

// WithMaxResponseBytes sets maximum response body size (default 2 MB).
func WithMaxResponseBytes(limit int64) Option {
	return func(f *Fetcher) {
		if limit > 0 {
			f.maxResponseBytes = limit
		}
	}
}

// WithMaxRetryAfter sets the maximum Retry-After duration tolerated before failing (default 60s).
func WithMaxRetryAfter(d time.Duration) Option {
	return func(f *Fetcher) {
		if d > 0 {
			f.maxRetryAfter = d
		}
	}
}

// WithLimiter integrates a shared Limiter for request pacing and global cool-down.
func WithLimiter(l Limiter) Option {
	return func(f *Fetcher) {
		f.limiter = l
	}
}

// WithAllowedHost adds an allowed redirect host (useful for test servers).
func WithAllowedHost(host string) Option {
	return func(f *Fetcher) {
		if host != "" {
			f.allowedHosts[strings.ToLower(host)] = true
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
	allowed := make(map[string]bool)
	for k, v := range defaultAllowedHosts {
		allowed[k] = v
	}

	f := &Fetcher{
		userAgent:        DefaultUserAgent,
		maxRetries:       DefaultMaxRetries,
		baseBackoff:      DefaultBaseBackoff,
		maxBackoff:       DefaultMaxBackoff,
		maxResponseBytes: DefaultMaxResponseSize,
		maxRetryAfter:    DefaultMaxRetryAfter,
		allowedHosts:     allowed,
		sleeper:          defaultContextSleep,
	}

	for _, opt := range opts {
		opt(f)
	}

	if f.client == nil {
		f.client = &http.Client{
			Timeout: DefaultTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= MaxRedirectHops {
					return ErrTooManyRedirects
				}

				// Refuse redirects to any host other than allowed skills.google hosts
				reqHost := strings.ToLower(req.URL.Hostname())
				if !f.allowedHosts[reqHost] {
					return fmt.Errorf("%w: %s", ErrUntrustedRedirect, reqHost)
				}

				lowerPath := strings.ToLower(req.URL.Path)
				if strings.Contains(lowerPath, "sign_in") || strings.Contains(lowerPath, "login") {
					return ErrNotFoundOrPrivate
				}
				return nil
			},
		}
	}

	return f
}

// Fetch loads the HTML content for the specified profile URL.
// It applies context timeouts, shared rate limiter tokens, global cool-down on 429/503,
// exponential backoff with jitter on transient errors, 2MB body limit, and redirect verification.
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
		// Acquire rate limiter token before EVERY attempt (initial + retries)
		if f.limiter != nil {
			if limitErr := f.limiter.Wait(ctx); limitErr != nil {
				return nil, fmt.Errorf("%w: %v", ErrNetwork, limitErr)
			}
		}

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
			if errors.Is(err, ErrNotFoundOrPrivate) {
				return nil, ErrNotFoundOrPrivate
			}
			if errors.Is(err, ErrUntrustedRedirect) {
				return nil, err
			}
			if errors.Is(err, ErrTooManyRedirects) {
				return nil, err
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
			}
			lastErr = err
			continue
		}

		// Inspect final landing URL
		finalPath := strings.ToLower(resp.Request.URL.Path)
		if strings.Contains(finalPath, "sign_in") || strings.Contains(finalPath, "login") {
			_ = resp.Body.Close()
			return nil, ErrNotFoundOrPrivate
		}

		switch {
		case resp.StatusCode == http.StatusOK:
			// Wrap response body in LimitReader (2 MB limit)
			limitReader := io.LimitReader(resp.Body, f.maxResponseBytes+1)
			body, readErr := io.ReadAll(limitReader)
			_ = resp.Body.Close()
			if readErr != nil {
				lastErr = fmt.Errorf("reading response body: %w", readErr)
				continue
			}
			if int64(len(body)) > f.maxResponseBytes {
				return nil, fmt.Errorf("%w (read %d bytes)", ErrResponseTooLarge, len(body))
			}
			return body, nil

		case resp.StatusCode == http.StatusNotFound:
			_ = resp.Body.Close()
			return nil, ErrNotFoundOrPrivate

		case resp.StatusCode == http.StatusTooManyRequests:
			wasRateLimited = true
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			_ = resp.Body.Close()

			// Retry-After clamp: max 60s by default. If server asks for more, return ErrRateLimited immediately without sleeping
			if retryAfter > f.maxRetryAfter {
				return nil, fmt.Errorf("%w: server requested retry-after %v exceeding max clamp %v",
					ErrRateLimited, retryAfter, f.maxRetryAfter)
			}

			// Pause shared rate limiter for all workers during the cool-down
			if f.limiter != nil && retryAfter > 0 {
				f.limiter.PauseFor(retryAfter)
			}

			lastErr = fmt.Errorf("HTTP 429 Too Many Requests (retry-after: %v)", retryAfter)
			if retryAfter > 0 {
				if sleepErr := f.sleeper(ctx, retryAfter); sleepErr != nil {
					return nil, fmt.Errorf("%w: %v", ErrNetwork, sleepErr)
				}
			}
			continue

		case resp.StatusCode >= 500 && resp.StatusCode <= 599:
			wasRateLimited = false
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			_ = resp.Body.Close()

			if retryAfter > f.maxRetryAfter {
				return nil, fmt.Errorf("%w: server requested retry-after %v exceeding max clamp %v",
					ErrNetwork, retryAfter, f.maxRetryAfter)
			}

			if f.limiter != nil && retryAfter > 0 {
				f.limiter.PauseFor(retryAfter)
			}

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
	multiplier := 1 << (attempt - 1)
	backoff := f.baseBackoff * time.Duration(multiplier)
	if backoff > f.maxBackoff {
		backoff = f.maxBackoff
	}

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

	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	if targetTime, err := http.ParseTime(header); err == nil {
		delta := time.Until(targetTime)
		if delta > 0 {
			return delta
		}
	}

	return 0
}
