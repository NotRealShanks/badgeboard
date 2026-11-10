package tracker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/cache"
	"github.com/NotRealShanks/badgeboard/internal/config"
	"github.com/NotRealShanks/badgeboard/internal/fetch"
	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/NotRealShanks/badgeboard/internal/parse"
	"github.com/NotRealShanks/badgeboard/internal/ratelimit"
	"github.com/NotRealShanks/badgeboard/internal/roster"
)

// ProgressFunc receives real-time completion events as each student profile finishes evaluation.
type ProgressFunc func(current, total int, result model.ProfileResult)

// Tracker coordinates fetching, parsing, caching, and rate limiting across a roster.
type Tracker struct {
	config  *config.Config
	fetcher *fetch.Fetcher
	cache   *cache.Cache
	limiter *ratelimit.Limiter
	refresh bool
}

// Option configures Tracker behavior.
type Option func(*Tracker)

// WithFetcher sets a custom Fetcher.
func WithFetcher(f *fetch.Fetcher) Option {
	return func(t *Tracker) {
		t.fetcher = f
	}
}

// WithCache sets a custom Cache.
func WithCache(c *cache.Cache) Option {
	return func(t *Tracker) {
		t.cache = c
	}
}

// WithRefresh forces bypassing the on-disk cache.
func WithRefresh(refresh bool) Option {
	return func(t *Tracker) {
		t.refresh = refresh
	}
}

// WithNoCache explicitly disables disk caching (read-nothing and write-nothing mode).
func WithNoCache() Option {
	return func(t *Tracker) {
		t.cache = nil
	}
}

// New creates an orchestrated Tracker configured from cfg.
func New(cfg *config.Config, opts ...Option) (*Tracker, error) {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	lim := ratelimit.New(cfg.RateLimit.RPS, cfg.RateLimit.Concurrency)

	f := fetch.New(
		fetch.WithLimiter(lim),
	)

	c, err := cache.New(cfg.Cache.Dir, time.Duration(cfg.Cache.TTLHours)*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("initializing cache: %w", err)
	}

	t := &Tracker{
		config:  cfg,
		fetcher: f,
		cache:   c,
		limiter: lim,
		refresh: false,
	}

	for _, opt := range opts {
		opt(t)
	}

	return t, nil
}

// Track processes all students in the roster using a bounded worker pool.
// Results strictly preserve the original roster order.
// Context cancellation stops scheduling new work while allowing in-flight workers to complete.
func (t *Tracker) Track(ctx context.Context, r *roster.Roster, onProgress ProgressFunc) ([]model.ProfileResult, error) {
	if r == nil || len(r.Students) == 0 {
		return nil, nil
	}

	total := len(r.Students)
	results := make([]model.ProfileResult, total)

	type task struct {
		index   int
		student model.Student
	}

	tasks := make(chan task, total)
	for i, s := range r.Students {
		tasks <- task{index: i, student: s}
	}
	close(tasks)

	concurrency := t.config.RateLimit.Concurrency
	if concurrency <= 0 {
		concurrency = ratelimit.DefaultConcurrency
	}

	var wg sync.WaitGroup
	var completedCount atomic.Int32
	var canceled atomic.Bool

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for tsk := range tasks {
				// If canceled, record partial context error and skip further dispatch
				if ctx.Err() != nil {
					canceled.Store(true)
					fallbackName := model.FallbackProfileLabel(tsk.student.UUID)
					results[tsk.index] = model.ProfileResult{
						Student:     tsk.student,
						DisplayName: fallbackName,
						Status:      model.StatusNetworkError,
						Error:       ctx.Err().Error(),
						FetchedAt:   model.TimePtr(time.Now().UTC()),
					}
					if results[tsk.index].Student.Name == "" {
						results[tsk.index].Student.Name = fallbackName
					}
					continue
				}

				res := t.processStudent(ctx, tsk.student)
				results[tsk.index] = res

				cur := completedCount.Add(1)
				if onProgress != nil {
					onProgress(int(cur), total, res)
				}
			}
		}()
	}

	wg.Wait()

	if canceled.Load() {
		return results, ctx.Err()
	}
	return results, nil
}

// TrackSingle processes a single student profile (used by check command).
func (t *Tracker) TrackSingle(ctx context.Context, student model.Student) model.ProfileResult {
	return t.processStudent(ctx, student)
}

func (t *Tracker) processStudent(ctx context.Context, student model.Student) model.ProfileResult {
	// 1. Check on-disk cache if refresh is not requested
	if !t.refresh && t.cache != nil && student.UUID != "" {
		if cached, hit, _ := t.cache.Get(student.UUID); hit && cached != nil {
			if cached.Status == model.StatusPrivateOrNotFound {
				res := model.ProfileResult{
					Student:   student,
					Status:    model.StatusPrivateOrNotFound,
					FetchedAt: model.TimePtr(cached.FetchedAt),
					FromCache: true,
				}
				if res.DisplayName == "" {
					res.DisplayName = model.FallbackProfileLabel(student.UUID)
				}
				if res.Student.Name == "" {
					res.Student.Name = res.DisplayName
				}
				return res
			}

			// Re-evaluate parsed profile data against current goal, required badges, and window
			data := parse.ProfileData{
				DisplayName: cached.DisplayName,
				League:      cached.League,
				Points:      cached.Points,
				Badges:      cached.Badges,
				IsPrivate:   cached.Status == model.StatusPrivateOrNotFound,
			}
			res := parse.Evaluate(&data, student, t.config.Goal, t.config.IsBadgeEligible, nil)
			res.FromCache = true
			res.FetchedAt = model.TimePtr(cached.FetchedAt)
			return res
		}
	}

	// 2. Fetch HTML over HTTP
	html, fetchErr := t.fetcher.Fetch(ctx, student.ProfileURL)
	now := time.Now().UTC()
	if fetchErr != nil {
		res := model.ProfileResult{
			Student:   student,
			FetchedAt: model.TimePtr(now),
			Error:     fetchErr.Error(),
		}

		switch {
		case errors.Is(fetchErr, fetch.ErrNotFoundOrPrivate):
			res.Status = model.StatusPrivateOrNotFound
			// Cache PrivateOrNotFound with short negative TTL
			if t.cache != nil && student.UUID != "" {
				_ = t.cache.Set(student.UUID, &cache.Entry{
					UUID:      student.UUID,
					Status:    model.StatusPrivateOrNotFound,
					FetchedAt: now,
				})
			}
		case errors.Is(fetchErr, fetch.ErrRateLimited):
			res.Status = model.StatusRateLimited
		case errors.Is(fetchErr, fetch.ErrNetwork), errors.Is(fetchErr, context.Canceled), errors.Is(fetchErr, context.DeadlineExceeded):
			res.Status = model.StatusNetworkError
		default:
			res.Status = model.StatusNetworkError
		}
		if res.DisplayName == "" {
			res.DisplayName = model.FallbackProfileLabel(student.UUID)
		}
		if res.Student.Name == "" {
			res.Student.Name = res.DisplayName
		}
		return res
	}

	// 3. Parse HTML bytes
	data, parseErr := parse.Parse(html)
	res := parse.Evaluate(data, student, t.config.Goal, t.config.IsBadgeEligible, parseErr)

	// 4. Save to cache if evaluation produced a cacheable 200 status (no raw HTML stored)
	if t.cache != nil && student.UUID != "" && parseErr == nil {
		fetched := now
		if res.FetchedAt != nil {
			fetched = *res.FetchedAt
		}
		_ = t.cache.Set(student.UUID, &cache.Entry{
			UUID:        student.UUID,
			Status:      res.Status,
			DisplayName: res.DisplayName,
			League:      res.League,
			Points:      res.Points,
			Badges:      res.Badges,
			FetchedAt:   fetched,
		})
	}

	return res
}

// WritesToCache returns the count of cache entries written to disk during this tracker run.
func (t *Tracker) WritesToCache() int {
	if t.cache == nil {
		return 0
	}
	return t.cache.WritesCount()
}

// CacheDir returns the directory path of the cache, if initialized.
func (t *Tracker) CacheDir() string {
	if t.cache == nil {
		return ""
	}
	return t.cache.Dir()
}
