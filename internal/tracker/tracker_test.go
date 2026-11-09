package tracker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/cache"
	"github.com/NotRealShanks/badgeboard/internal/config"
	"github.com/NotRealShanks/badgeboard/internal/fetch"
	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/NotRealShanks/badgeboard/internal/roster"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func loadRealSampleHTML(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "sample_profile.html")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed reading sample profile: %v", err)
	}
	return b
}

func TestTracker_EmptyRoster(t *testing.T) {
	trk, err := New(nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	res, err := trk.Track(context.Background(), nil, nil)
	if err != nil || res != nil {
		t.Errorf("expected nil, nil for nil roster, got: %v, %v", res, err)
	}

	res, err = trk.Track(context.Background(), &roster.Roster{}, nil)
	if err != nil || res != nil {
		t.Errorf("expected nil, nil for empty roster, got: %v, %v", res, err)
	}
}

func TestTracker_ErrorHandling_PrivateAndRateLimited(t *testing.T) {
	tmpDir := t.TempDir()
	c, _ := cache.New(tmpDir, 6*time.Hour)

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/public_profiles/404":
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(bytes.NewBufferString("not found")),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			case "/public_profiles/429":
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Body:       io.NopCloser(bytes.NewBufferString("rate limited")),
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
		}),
	}

	cfg := &config.Config{
		Goal: 10,
		RateLimit: config.RateLimitConfig{
			RPS:         100.0,
			Concurrency: 1,
		},
	}

	fastFetcher := fetch.New(
		fetch.WithHTTPClient(client),
		fetch.WithMaxRetries(0), // Fail fast
		fetch.WithSleeper(func(_ context.Context, _ time.Duration) error { return nil }),
	)

	trk, _ := New(cfg, WithFetcher(fastFetcher), WithCache(c))

	// 1. Test 404 maps to StatusPrivateOrNotFound and is cached with short negative TTL
	res404 := trk.TrackSingle(context.Background(), model.Student{
		UUID:       "404",
		ProfileURL: "https://www.skills.google/public_profiles/404",
	})
	if res404.Status != model.StatusPrivateOrNotFound {
		t.Errorf("expected StatusPrivateOrNotFound, got %v", res404.Status)
	}
	cached404, hit, _ := c.Get("404")
	if !hit || cached404.Status != model.StatusPrivateOrNotFound {
		t.Errorf("expected cached negative entry for 404, got hit=%v", hit)
	}

	// 2. Second hit on 404 returns FromCache=true
	resCached404 := trk.TrackSingle(context.Background(), model.Student{
		UUID:       "404",
		ProfileURL: "https://www.skills.google/public_profiles/404",
	})
	if !resCached404.FromCache || resCached404.Status != model.StatusPrivateOrNotFound {
		t.Errorf("expected FromCache=true on cached 404, got %+v", resCached404)
	}

	// 3. Test 429 maps to StatusRateLimited
	res429 := trk.TrackSingle(context.Background(), model.Student{
		UUID:       "429",
		ProfileURL: "https://www.skills.google/public_profiles/429",
	})
	if res429.Status != model.StatusRateLimited {
		t.Errorf("expected StatusRateLimited, got %v", res429.Status)
	}

	// 4. Test 500 maps to StatusNetworkError
	res500 := trk.TrackSingle(context.Background(), model.Student{
		UUID:       "500",
		ProfileURL: "https://www.skills.google/public_profiles/500",
	})
	if res500.Status != model.StatusNetworkError {
		t.Errorf("expected StatusNetworkError, got %v", res500.Status)
	}
}

func TestTracker_OrderPreservationAndWorkerPool(t *testing.T) {
	sampleHTML := loadRealSampleHTML(t)

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(sampleHTML)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}

	cfg := &config.Config{
		Goal: 10,
		RateLimit: config.RateLimitConfig{
			RPS:         100.0,
			Concurrency: 4,
		},
		Cache: config.CacheConfig{
			TTLHours: 6,
			Dir:      t.TempDir(),
		},
	}

	fastFetcher := fetch.New(
		fetch.WithHTTPClient(client),
		fetch.WithSleeper(func(_ context.Context, _ time.Duration) error { return nil }),
	)

	trk, err := New(cfg, WithFetcher(fastFetcher))
	if err != nil {
		t.Fatalf("failed to create tracker: %v", err)
	}

	students := make([]model.Student, 10)
	for i := 0; i < 10; i++ {
		uuid := fmt.Sprintf("11111111-2222-3333-4444-%012d", i)
		students[i] = model.Student{
			Name:       fmt.Sprintf("Student %d", i),
			UUID:       uuid,
			ProfileURL: fmt.Sprintf("https://www.skills.google/public_profiles/%s", uuid),
			RowNumber:  i + 2,
		}
	}

	rost := &roster.Roster{Students: students}

	var progressCount atomic.Int32
	results, err := trk.Track(context.Background(), rost, func(cur, tot int, r model.ProfileResult) {
		progressCount.Add(1)
	})
	if err != nil {
		t.Fatalf("Track failed: %v", err)
	}

	if len(results) != 10 {
		t.Fatalf("expected 10 results, got %d", len(results))
	}
	if progressCount.Load() != 10 {
		t.Errorf("expected 10 progress calls, got %d", progressCount.Load())
	}

	for i, res := range results {
		expectedUUID := fmt.Sprintf("11111111-2222-3333-4444-%012d", i)
		if res.Student.UUID != expectedUUID {
			t.Errorf("order mismatch at index %d: got %s, want %s", i, res.Student.UUID, expectedUUID)
		}
		if res.Status != model.StatusComplete {
			t.Errorf("expected StatusComplete at index %d, got %v", i, res.Status)
		}
		if res.CountedBadges != 10 {
			t.Errorf("expected 10 counted badges at index %d, got %d", i, res.CountedBadges)
		}
	}
}

func TestTracker_CacheHitAndRefreshBypass(t *testing.T) {
	sampleHTML := loadRealSampleHTML(t)
	var networkRequests atomic.Int32

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			networkRequests.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(sampleHTML)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}

	tmpDir := t.TempDir()
	c, _ := cache.New(tmpDir, 6*time.Hour)

	cfg := &config.Config{
		Goal: 10,
		RateLimit: config.RateLimitConfig{
			RPS:         100.0,
			Concurrency: 2,
		},
	}

	fastFetcher := fetch.New(
		fetch.WithHTTPClient(client),
		fetch.WithSleeper(func(_ context.Context, _ time.Duration) error { return nil }),
	)

	student := model.Student{
		Name:       "Cache User",
		UUID:       "be556f7b-ab4b-4be7-82fb-9be45202ddd7",
		ProfileURL: "https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7",
	}
	rost := &roster.Roster{Students: []model.Student{student}}

	trk1, _ := New(cfg, WithFetcher(fastFetcher), WithCache(c))
	res1, err := trk1.Track(context.Background(), rost, nil)
	if err != nil || len(res1) != 1 {
		t.Fatalf("first Track run failed: %v", err)
	}
	if res1[0].FromCache {
		t.Errorf("expected FromCache=false on first run")
	}
	if networkRequests.Load() != 1 {
		t.Errorf("expected 1 network request on first run, got %d", networkRequests.Load())
	}

	trk2, _ := New(cfg, WithFetcher(fastFetcher), WithCache(c))
	res2, err := trk2.Track(context.Background(), rost, nil)
	if err != nil || len(res2) != 1 {
		t.Fatalf("second Track run failed: %v", err)
	}
	if !res2[0].FromCache {
		t.Errorf("expected FromCache=true on second run")
	}
	if networkRequests.Load() != 1 {
		t.Errorf("expected network requests to stay at 1, got %d", networkRequests.Load())
	}

	trk3, _ := New(cfg, WithFetcher(fastFetcher), WithCache(c), WithRefresh(true))
	res3, err := trk3.Track(context.Background(), rost, nil)
	if err != nil || len(res3) != 1 {
		t.Fatalf("third Track run failed: %v", err)
	}
	if res3[0].FromCache {
		t.Errorf("expected FromCache=false when refresh=true")
	}
	if networkRequests.Load() != 2 {
		t.Errorf("expected network requests to reach 2 on refresh, got %d", networkRequests.Load())
	}
}

func TestTracker_ContextCancellationGraceful(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			time.Sleep(50 * time.Millisecond)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("ok")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}

	cfg := &config.Config{
		Goal: 10,
		RateLimit: config.RateLimitConfig{
			RPS:         10.0,
			Concurrency: 1,
		},
		Cache: config.CacheConfig{
			Dir: t.TempDir(),
		},
	}

	fastFetcher := fetch.New(fetch.WithHTTPClient(client))
	trk, _ := New(cfg, WithFetcher(fastFetcher))

	students := make([]model.Student, 5)
	for i := 0; i < 5; i++ {
		uuid := fmt.Sprintf("canc-%012d", i)
		students[i] = model.Student{
			UUID:       uuid,
			ProfileURL: "https://www.skills.google/public_profiles/" + uuid,
		}
	}
	rost := &roster.Roster{Students: students}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)

	results, err := trk.Track(ctx, rost, nil)
	if err == nil {
		t.Errorf("expected context cancellation error, got nil")
	}

	if len(results) != 5 {
		t.Errorf("expected 5 result slots returned, got %d", len(results))
	}
}

func TestTracker_TrackSingle(t *testing.T) {
	sampleHTML := loadRealSampleHTML(t)
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(sampleHTML)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}

	fastFetcher := fetch.New(fetch.WithHTTPClient(client))
	trk, _ := New(nil, WithFetcher(fastFetcher))

	student := model.Student{
		Name:       "Check User",
		UUID:       "be556f7b-ab4b-4be7-82fb-9be45202ddd7",
		ProfileURL: "https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7",
	}

	res := trk.TrackSingle(context.Background(), student)
	if res.Status != model.StatusComplete {
		t.Errorf("expected StatusComplete, got %v", res.Status)
	}
	if res.CountedBadges != 10 {
		t.Errorf("expected 10 badges, got %d", res.CountedBadges)
	}
}
