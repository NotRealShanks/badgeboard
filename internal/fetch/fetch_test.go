package fetch

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func mockClient(fn roundTripFunc, checkRedirect func(req *http.Request, via []*http.Request) error) *http.Client {
	if checkRedirect == nil {
		checkRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= MaxRedirectHops {
				return ErrTooManyRedirects
			}
			reqHost := stringsToLower(req.URL.Hostname())
			if !defaultAllowedHosts[reqHost] && reqHost != "test-allowed.google" {
				return ErrUntrustedRedirect
			}
			lowerPath := stringsToLower(req.URL.Path)
			if stringsContains(lowerPath, "sign_in") || stringsContains(lowerPath, "login") {
				return ErrNotFoundOrPrivate
			}
			return nil
		}
	}
	return &http.Client{
		Transport:     fn,
		CheckRedirect: checkRedirect,
	}
}

func stringsToLower(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b = append(b, c)
	}
	return string(b)
}

func stringsContains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && bytes.Contains([]byte(s), []byte(substr)))
}

func fastTestSleeper(_ context.Context, _ time.Duration) error {
	return nil
}

type mockLimiter struct {
	waitCount  atomic.Int32
	pauseCount atomic.Int32
	lastPause  time.Duration
}

func (m *mockLimiter) Wait(ctx context.Context) error {
	m.waitCount.Add(1)
	return ctx.Err()
}

func (m *mockLimiter) PauseFor(d time.Duration) {
	m.pauseCount.Add(1)
	m.lastPause = d
}

func TestFetch_WithSharedLimiterAndCoolDown(t *testing.T) {
	lim := &mockLimiter{}
	var attempts int32

	client := mockClient(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			hdr := make(http.Header)
			hdr.Set("Retry-After", "5")
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     hdr,
				Body:       io.NopCloser(bytes.NewBufferString("throttled")),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("recovered")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithLimiter(lim),
		WithSleeper(fastTestSleeper),
		WithMaxRetries(2),
	)

	body, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "recovered" {
		t.Errorf("unexpected body: %s", string(body))
	}

	// Limiter.Wait must be called for BOTH attempt 1 and retry attempt 2
	if lim.waitCount.Load() != 2 {
		t.Errorf("expected 2 limiter Wait calls, got %d", lim.waitCount.Load())
	}
	// Limiter.PauseFor must be called with Retry-After (5s)
	if lim.pauseCount.Load() != 1 || lim.lastPause != 5*time.Second {
		t.Errorf("expected limiter paused for 5s, got %v (count: %d)", lim.lastPause, lim.pauseCount.Load())
	}
}

func TestFetch_OptionsAndDefaults(t *testing.T) {
	f := New(
		WithUserAgent("custom-agent"),
		WithMaxRetries(5),
		WithBackoff(100*time.Millisecond, 5*time.Second),
		WithMaxResponseBytes(1024*1024),
		WithMaxRetryAfter(30*time.Second),
		WithAllowedHost("custom.skills.google"),
		WithHTTPClient(nil),
	)

	if f.userAgent != "custom-agent" {
		t.Errorf("expected custom-agent, got %s", f.userAgent)
	}
	if f.maxRetries != 5 {
		t.Errorf("expected maxRetries 5, got %d", f.maxRetries)
	}
	if f.baseBackoff != 100*time.Millisecond {
		t.Errorf("expected baseBackoff 100ms, got %v", f.baseBackoff)
	}
	if f.maxBackoff != 5*time.Second {
		t.Errorf("expected maxBackoff 5s, got %v", f.maxBackoff)
	}
	if f.maxResponseBytes != 1024*1024 {
		t.Errorf("expected 1MB limit, got %d", f.maxResponseBytes)
	}
	if f.maxRetryAfter != 30*time.Second {
		t.Errorf("expected 30s max retry after, got %v", f.maxRetryAfter)
	}

	b1 := f.calculateBackoff(1, nil)
	if b1 <= 0 || b1 > 100*time.Millisecond {
		t.Errorf("b1 out of expected range: %v", b1)
	}
	bHigh := f.calculateBackoff(10, nil)
	if bHigh > 5*time.Second {
		t.Errorf("expected backoff capped at maxBackoff, got: %v", bHigh)
	}
}

func TestFetch_ParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter(""); d != 0 {
		t.Errorf("expected 0 for empty header, got %v", d)
	}
	if d := parseRetryAfter("invalid"); d != 0 {
		t.Errorf("expected 0 for invalid header, got %v", d)
	}
	if d := parseRetryAfter("-5"); d != 0 {
		t.Errorf("expected 0 for negative seconds, got %v", d)
	}
	pastDate := time.Now().Add(-10 * time.Minute).UTC().Format(http.TimeFormat)
	if d := parseRetryAfter(pastDate); d != 0 {
		t.Errorf("expected 0 for past HTTP date, got %v", d)
	}
}

func TestFetch_LimitReader_OversizedBodyRefused(t *testing.T) {
	oversizedBytes := make([]byte, 2*1024*1024+1024)

	client := mockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(oversizedBytes)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
	)

	_, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/oversized")
	if err == nil {
		t.Fatalf("expected error on oversized body, got nil")
	}
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Errorf("expected ErrResponseTooLarge, got: %v", err)
	}
}

func TestFetch_Redirect_UntrustedHostRefused(t *testing.T) {
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		hdr := make(http.Header)
		hdr.Set("Location", "https://evil-phishing.com/steal-creds")
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     hdr,
			Body:       io.NopCloser(bytes.NewBufferString("redirecting to untrusted host")),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
	)

	_, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/redirect-untrusted")
	if err == nil {
		t.Fatalf("expected error on redirect to untrusted host, got nil")
	}
	if !errors.Is(err, ErrUntrustedRedirect) {
		t.Errorf("expected ErrUntrustedRedirect, got: %v", err)
	}
}

func TestFetch_Redirect_CappedAt5Hops(t *testing.T) {
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		hdr := make(http.Header)
		hdr.Set("Location", "https://www.skills.google/public_profiles/loop")
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     hdr,
			Body:       io.NopCloser(bytes.NewBufferString("redirect loop")),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
	)

	_, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/loop")
	if err == nil {
		t.Fatalf("expected error for excessive redirect hops, got nil")
	}
	if !errors.Is(err, ErrTooManyRedirects) {
		t.Errorf("expected ErrTooManyRedirects, got: %v", err)
	}
}

func TestFetch_RetryAfterClamp_ExceedsMaxFailsImmediately(t *testing.T) {
	var attempts int32
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&attempts, 1)
		hdr := make(http.Header)
		hdr.Set("Retry-After", "120") // 120s exceeds default 60s clamp
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     hdr,
			Body:       io.NopCloser(bytes.NewBufferString("rate limited")),
			Request:    req,
		}, nil
	}, nil)

	slept := false
	sleepRecorder := func(_ context.Context, _ time.Duration) error {
		slept = true
		return nil
	}

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(sleepRecorder),
		WithMaxRetryAfter(60*time.Second),
	)

	_, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/test")
	if err == nil {
		t.Fatalf("expected error on retry-after exceeding clamp, got nil")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got: %v", err)
	}
	if slept {
		t.Errorf("fetcher should fail immediately without sleeping when Retry-After exceeds clamp")
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected exactly 1 attempt before immediate failure, got %d", attempts)
	}
}

func TestFetch_ServerError5xx_RetryAfterClamp(t *testing.T) {
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		hdr := make(http.Header)
		hdr.Set("Retry-After", "999") // exceeds 60s
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     hdr,
			Body:       io.NopCloser(bytes.NewBufferString("service unavailable")),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
		WithMaxRetryAfter(60*time.Second),
	)

	_, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/test")
	if err == nil {
		t.Fatalf("expected error on 503 retry-after exceeding clamp, got nil")
	}
	if !errors.Is(err, ErrNetwork) {
		t.Errorf("expected ErrNetwork, got: %v", err)
	}
}

func TestFetch_SuccessAndUserAgent(t *testing.T) {
	var capturedUA string
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		capturedUA = req.Header.Get("User-Agent")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("<html>Sample Profile</html>")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
	)

	body, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/1111")
	if err != nil {
		t.Fatalf("unexpected fetch error: %v", err)
	}

	if string(body) != "<html>Sample Profile</html>" {
		t.Errorf("unexpected body: %s", string(body))
	}

	if capturedUA != DefaultUserAgent {
		t.Errorf("expected User-Agent %q, got %q", DefaultUserAgent, capturedUA)
	}
}

func TestFetch_NotFound_MapsToErrNotFoundOrPrivate(t *testing.T) {
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewBufferString("404 Not Found")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
	)

	_, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/missing")
	if err == nil {
		t.Fatalf("expected error on 404, got nil")
	}
	if !errors.Is(err, ErrNotFoundOrPrivate) {
		t.Errorf("expected ErrNotFoundOrPrivate for 404, got: %v", err)
	}
}

func TestFetch_RedirectToSignIn_MapsToErrNotFoundOrPrivate(t *testing.T) {
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/users/sign_in" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("Sign in")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		hdr := make(http.Header)
		hdr.Set("Location", "https://www.skills.google/users/sign_in")
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     hdr,
			Body:       io.NopCloser(bytes.NewBufferString("Redirecting")),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
	)

	_, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/private-user")
	if err == nil {
		t.Fatalf("expected error on sign-in redirect, got nil")
	}
	if !errors.Is(err, ErrNotFoundOrPrivate) {
		t.Errorf("expected ErrNotFoundOrPrivate for sign-in redirect, got: %v", err)
	}
}

func TestFetch_FinalURLContainsSignIn_MapsToErrNotFoundOrPrivate(t *testing.T) {
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		landingURL, _ := url.Parse("https://www.skills.google/users/sign_in")
		reqWithFinal := req.Clone(context.Background())
		reqWithFinal.URL = landingURL
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("Landing page")),
			Header:     make(http.Header),
			Request:    reqWithFinal,
		}, nil
	}, func(req *http.Request, via []*http.Request) error {
		return nil
	})

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
	)

	_, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/private-user")
	if !errors.Is(err, ErrNotFoundOrPrivate) {
		t.Errorf("expected ErrNotFoundOrPrivate when final landing URL is sign_in, got: %v", err)
	}
}

func TestFetch_RetryAfter_429DeltaSeconds(t *testing.T) {
	var attempts int32
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			hdr := make(http.Header)
			hdr.Set("Retry-After", "2")
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     hdr,
				Body:       io.NopCloser(bytes.NewBufferString("rate limited")),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("Success after rate limit")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}, nil)

	var sleptDurations []time.Duration
	var mu sync.Mutex
	recordSleeper := func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		sleptDurations = append(sleptDurations, d)
		return nil
	}

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(recordSleeper),
		WithMaxRetries(2),
	)

	body, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/test")
	if err != nil {
		t.Fatalf("expected recovery after 429, got: %v", err)
	}
	if string(body) != "Success after rate limit" {
		t.Errorf("unexpected body: %s", string(body))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sleptDurations) == 0 || sleptDurations[0] != 2*time.Second {
		t.Errorf("expected 2s retry-after sleep, got: %v", sleptDurations)
	}
}

func TestFetch_ServerError500_RetryAndRecovery(t *testing.T) {
	var attempts int32
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= 2 {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewBufferString("500")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("Recovered after 500 errors")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
		WithMaxRetries(3),
	)

	body, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/test")
	if err != nil {
		t.Fatalf("expected recovery, got error: %v", err)
	}
	if string(body) != "Recovered after 500 errors" {
		t.Errorf("unexpected body: %s", string(body))
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestFetch_Persistent5xx_ReturnsErrNetwork(t *testing.T) {
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(bytes.NewBufferString("502")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
		WithMaxRetries(2),
	)

	_, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/test")
	if err == nil {
		t.Fatalf("expected error on persistent 502, got nil")
	}
	if !errors.Is(err, ErrNetwork) {
		t.Errorf("expected ErrNetwork for exhausted 5xx, got: %v", err)
	}
}

func TestFetch_UnexpectedStatusCode_ReturnsErrNetwork(t *testing.T) {
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(bytes.NewBufferString("400 Bad Request")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
	)

	_, err := fetcher.Fetch(context.Background(), "https://www.skills.google/public_profiles/test")
	if err == nil {
		t.Fatalf("expected error on 400, got nil")
	}
	if !errors.Is(err, ErrNetwork) {
		t.Errorf("expected ErrNetwork for 400 status, got: %v", err)
	}
}

func TestFetch_ContextCancellation(t *testing.T) {
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("ok")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetcher.Fetch(ctx, "https://www.skills.google/public_profiles/test")
	if err == nil {
		t.Fatalf("expected error with canceled context, got nil")
	}
	if !errors.Is(err, ErrNetwork) {
		t.Errorf("expected ErrNetwork wrapping canceled context, got: %v", err)
	}
}

func TestFetch_MixedBatch_OneFailureDoesNotAffectOthers(t *testing.T) {
	client := mockClient(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/user/ok1":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("profile ok 1")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "/user/404":
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewBufferString("not found")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "/user/ok2":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("profile ok 2")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewBufferString("500")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	}, nil)

	fetcher := New(
		WithHTTPClient(client),
		WithSleeper(fastTestSleeper),
		WithMaxRetries(1),
	)

	type result struct {
		path string
		body string
		err  error
	}

	urls := []string{"/user/ok1", "/user/404", "/user/ok2"}
	results := make([]result, len(urls))
	var wg sync.WaitGroup

	for i, p := range urls {
		wg.Add(1)
		go func(idx int, path string) {
			defer wg.Done()
			b, err := fetcher.Fetch(context.Background(), "https://www.skills.google"+path)
			results[idx] = result{path: path, body: string(b), err: err}
		}(i, p)
	}

	wg.Wait()

	if results[0].err != nil || results[0].body != "profile ok 1" {
		t.Errorf("ok1 failed: %v", results[0])
	}
	if !errors.Is(results[1].err, ErrNotFoundOrPrivate) {
		t.Errorf("expected ErrNotFoundOrPrivate for 404, got: %v", results[1].err)
	}
	if results[2].err != nil || results[2].body != "profile ok 2" {
		t.Errorf("ok2 failed: %v", results[2])
	}
}
