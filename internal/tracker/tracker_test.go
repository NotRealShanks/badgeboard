package tracker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

func TestTracker_CacheReEvaluation_GoalRequiredBadgesAndWindow(t *testing.T) {
	tmpDir := t.TempDir()
	c, err := cache.New(tmpDir, 6*time.Hour)
	if err != nil {
		t.Fatalf("cache.New failed: %v", err)
	}

	uuid := "cache-reeval-uuid"
	t1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

	// Pre-populate disk cache with parsed profile data (no raw HTML)
	cachedEntry := &cache.Entry{
		UUID:        uuid,
		Status:      model.StatusInProgress,
		DisplayName: "Sample Learner",
		League:      "Gold League",
		Points:      200,
		Badges: []model.BadgeRecord{
			{ID: "b1", Title: "Cloud Basics", EarnedAt: t1, RawEarned: "Earned Nov 1, 2026 EST"},
			{ID: "b2", Title: "Generative AI", EarnedAt: t2, RawEarned: "Earned Nov 5, 2026 EST"},
			{ID: "b3", Title: "Security Best Practices", EarnedAt: t3, RawEarned: "Earned Nov 10, 2026 EST"},
		},
		FetchedAt: time.Now().UTC(),
	}
	if err := c.Set(uuid, cachedEntry); err != nil {
		t.Fatalf("seeding cache failed: %v", err)
	}

	var networkReqs atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			networkReqs.Add(1)
			t.Errorf("network request made when cache hit expected: %s", req.URL.String())
			return nil, fmt.Errorf("unexpected network request")
		}),
	}
	f := fetch.New(fetch.WithHTTPClient(client))

	student := model.Student{
		UUID:       uuid,
		ProfileURL: "https://www.skills.google/public_profiles/" + uuid,
	}
	rost := &roster.Roster{Students: []model.Student{student}}

	// 1. Goal 3 -> StatusComplete (3/3)
	cfgGoal3 := &config.Config{Goal: 3}
	trkGoal3, _ := New(cfgGoal3, WithFetcher(f), WithCache(c))
	resGoal3, err := trkGoal3.Track(context.Background(), rost, nil)
	if err != nil || len(resGoal3) != 1 {
		t.Fatalf("Goal 3 run failed: %v", err)
	}
	if !resGoal3[0].FromCache {
		t.Errorf("expected FromCache=true")
	}
	if resGoal3[0].Status != model.StatusComplete || resGoal3[0].CountedBadges != 3 {
		t.Errorf("expected StatusComplete with 3 badges, got: %s (%d)", resGoal3[0].Status, resGoal3[0].CountedBadges)
	}

	// 2. Goal 5 -> StatusInProgress (3/5) without network request
	cfgGoal5 := &config.Config{Goal: 5}
	trkGoal5, _ := New(cfgGoal5, WithFetcher(f), WithCache(c))
	resGoal5, err := trkGoal5.Track(context.Background(), rost, nil)
	if err != nil || len(resGoal5) != 1 {
		t.Fatalf("Goal 5 run failed: %v", err)
	}
	if resGoal5[0].Status != model.StatusInProgress || resGoal5[0].CountedBadges != 3 {
		t.Errorf("expected StatusInProgress with 3/5 badges, got: %s (%d)", resGoal5[0].Status, resGoal5[0].CountedBadges)
	}

	// 3. Required badges filter -> only "Cloud Basics" counts
	cfgReq := &config.Config{
		Goal:           1,
		RequiredBadges: []string{"Cloud Basics"},
	}
	trkReq, _ := New(cfgReq, WithFetcher(f), WithCache(c))
	resReq, err := trkReq.Track(context.Background(), rost, nil)
	if err != nil || len(resReq) != 1 {
		t.Fatalf("Required badges run failed: %v", err)
	}
	if resReq[0].CountedBadges != 1 || resReq[0].TotalBadges != 3 {
		t.Errorf("expected CountedBadges=1, TotalBadges=3, got counted=%d, total=%d", resReq[0].CountedBadges, resReq[0].TotalBadges)
	}
	if resReq[0].Status != model.StatusComplete {
		t.Errorf("expected StatusComplete for 1 required badge with goal 1, got %s", resReq[0].Status)
	}

	// 4. Campaign window filter: window since Nov 4, 2026 -> only 2 badges count (Nov 5 and Nov 10)
	windowSince := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	cfgWindow := &config.Config{
		Goal: 3,
		Window: &config.DateWindow{
			Start: &windowSince,
		},
	}
	trkWindow, _ := New(cfgWindow, WithFetcher(f), WithCache(c))
	resWindow, err := trkWindow.Track(context.Background(), rost, nil)
	if err != nil || len(resWindow) != 1 {
		t.Fatalf("Campaign window run failed: %v", err)
	}
	if resWindow[0].CountedBadges != 2 || resWindow[0].TotalBadges != 3 {
		t.Errorf("expected CountedBadges=2, TotalBadges=3 under date window, got counted=%d, total=%d", resWindow[0].CountedBadges, resWindow[0].TotalBadges)
	}

	// Verify ZERO network requests were made across all 4 evaluations
	if networkReqs.Load() != 0 {
		t.Errorf("expected 0 network requests during cache re-evaluation, got %d", networkReqs.Load())
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
	cfg := config.DefaultConfig()
	cfg.Cache.Dir = t.TempDir()
	trk, _ := New(cfg, WithFetcher(fastFetcher))

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

func TestTracker_InterruptedRunResumption(t *testing.T) {
	sampleHTML := loadRealSampleHTML(t)
	var networkRequests atomic.Int32
	var s0Fetched atomic.Bool

	cacheDir := t.TempDir()
	c, err := cache.New(cacheDir, 6*time.Hour)
	if err != nil {
		t.Fatalf("failed creating disk cache: %v", err)
	}

	cfg := &config.Config{
		Goal: 10,
		RateLimit: config.RateLimitConfig{
			RPS:         100.0,
			Concurrency: 1, // Single worker guarantees deterministic serial execution
		},
	}

	students := []model.Student{
		{
			Name:       "Student 0",
			UUID:       "be556f7b-ab4b-4be7-82fb-9be45202ddd7",
			ProfileURL: "https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7",
		},
		{
			Name:       "Student 1",
			UUID:       "8f0d00c3-7674-4042-ba15-d26a3fae84f9",
			ProfileURL: "https://www.skills.google/public_profiles/8f0d00c3-7674-4042-ba15-d26a3fae84f9",
		},
		{
			Name:       "Student 2",
			UUID:       "2fafe410-5b6c-463b-9a1d-61dbfb43e9ac",
			ProfileURL: "https://www.skills.google/public_profiles/2fafe410-5b6c-463b-9a1d-61dbfb43e9ac",
		},
	}
	rost := &roster.Roster{Students: students}

	ctx1, cancel1 := context.WithCancel(context.Background())

	client1 := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			networkRequests.Add(1)
			if strings.Contains(req.URL.Path, "be556f7b-ab4b-4be7-82fb-9be45202ddd7") {
				s0Fetched.Store(true)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(sampleHTML)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	fetcher1 := fetch.New(
		fetch.WithHTTPClient(client1),
		fetch.WithSleeper(func(_ context.Context, _ time.Duration) error { return nil }),
	)

	trk1, err := New(cfg, WithFetcher(fetcher1), WithCache(c))
	if err != nil {
		t.Fatalf("failed creating tracker 1: %v", err)
	}

	// Cancel context as soon as Student 0 finishes
	_, err1 := trk1.Track(ctx1, rost, func(cur, tot int, r model.ProfileResult) {
		if r.Student.UUID == students[0].UUID {
			cancel1()
		}
	})
	if err1 == nil {
		t.Errorf("expected context cancellation error on interrupted run, got nil")
	}

	if !s0Fetched.Load() {
		t.Fatalf("expected student 0 to have been fetched before cancellation")
	}

	// Verify Student 0 was persisted to disk cache
	cachedS0, found, err := c.Get(students[0].UUID)
	if err != nil || !found || cachedS0 == nil {
		t.Fatalf("expected student 0 to be present in disk cache after interruption: found=%v, err=%v", found, err)
	}

	// Reset network counter to verify second run behavior
	initialReqs := networkRequests.Load()

	// Second run: fresh tracker with the SAME disk cache and no cancellation
	client2 := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, students[0].UUID) {
				t.Errorf("FATAL: student 0 re-fetched from network instead of disk cache!")
			}
			networkRequests.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(sampleHTML)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	fetcher2 := fetch.New(
		fetch.WithHTTPClient(client2),
		fetch.WithSleeper(func(_ context.Context, _ time.Duration) error { return nil }),
	)

	trk2, err := New(cfg, WithFetcher(fetcher2), WithCache(c))
	if err != nil {
		t.Fatalf("failed creating tracker 2: %v", err)
	}

	results2, err2 := trk2.Track(context.Background(), rost, nil)
	if err2 != nil {
		t.Fatalf("resumed run failed: %v", err2)
	}

	if len(results2) != 3 {
		t.Fatalf("expected 3 results on resumed run, got %d", len(results2))
	}

	// Student 0 MUST be from cache
	if !results2[0].FromCache {
		t.Errorf("expected student 0 FromCache=true on resumed run")
	}
	if results2[0].Status != model.StatusComplete {
		t.Errorf("expected student 0 StatusComplete, got %v", results2[0].Status)
	}

	// Total additional network requests should only be for remaining students (students 1 & 2)
	newReqs := networkRequests.Load() - initialReqs
	if newReqs > 2 {
		t.Errorf("expected at most 2 new network requests on resumption, got %d", newReqs)
	}
}

