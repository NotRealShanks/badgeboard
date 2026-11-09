package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	DefaultRPS         = 1.0
	DefaultConcurrency = 2
)

// Clock defines a time provider interface for deterministic testing.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

func (realClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

// Option allows configuring Limiter instances.
type Option func(*Limiter)

// WithClock sets a custom Clock for deterministic testing.
func WithClock(c Clock) Option {
	return func(l *Limiter) {
		if c != nil {
			l.clock = c
		}
	}
}

// Limiter coordinates request rates, concurrency bounds, and global cool-down pauses.
type Limiter struct {
	rps         float64
	concurrency int
	clock       Clock
	rateLimiter *rate.Limiter
	sem         chan struct{}
	pausedUntil time.Time
	mu          sync.Mutex
}

// New creates a new Limiter with the specified RPS and concurrency limits.
// Zero or negative values fall back to defaults (1 request/sec, concurrency 2).
func New(rps float64, concurrency int, opts ...Option) *Limiter {
	if rps <= 0 {
		rps = DefaultRPS
	}
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}

	lim := &Limiter{
		rps:         rps,
		concurrency: concurrency,
		clock:       realClock{},
		rateLimiter: rate.NewLimiter(rate.Limit(rps), 1),
		sem:         make(chan struct{}, concurrency),
	}

	for _, opt := range opts {
		opt(lim)
	}

	return lim
}

// PauseUntil sets a global cool-down deadline across all workers sharing this limiter.
// No worker can proceed through Wait() until the specified time has elapsed.
func (l *Limiter) PauseUntil(until time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until.After(l.pausedUntil) {
		l.pausedUntil = until
	}
}

// PauseFor pauses all workers sharing this limiter for the given duration.
func (l *Limiter) PauseFor(d time.Duration) {
	if d > 0 {
		l.PauseUntil(l.clock.Now().Add(d))
	}
}

// IsPaused returns true if the global cool-down is currently active.
func (l *Limiter) IsPaused() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.clock.Now().Before(l.pausedUntil)
}

// Wait blocks until any global cool-down has expired AND the rate limiter permits an action.
func (l *Limiter) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// 1. Check for global cool-down pause
	l.mu.Lock()
	now := l.clock.Now()
	var pauseDelay time.Duration
	if now.Before(l.pausedUntil) {
		pauseDelay = l.pausedUntil.Sub(now)
	}
	l.mu.Unlock()

	if pauseDelay > 0 {
		if err := l.sleep(ctx, pauseDelay); err != nil {
			return err
		}
	}

	// 2. Reserve from underlying rate limiter
	l.mu.Lock()
	now = l.clock.Now()
	r := l.rateLimiter.ReserveN(now, 1)
	if !r.OK() {
		l.mu.Unlock()
		return fmt.Errorf("rate limiter burst capacity exceeded")
	}
	delay := r.DelayFrom(now)
	l.mu.Unlock()

	if delay <= 0 {
		return nil
	}

	return l.sleep(ctx, delay)
}

func (l *Limiter) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	if _, isReal := l.clock.(realClock); !isReal {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.clock.Sleep(d)
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Acquire acquires a concurrency slot, blocking if the active worker limit is reached.
func (l *Limiter) Acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case l.sem <- struct{}{}:
		return nil
	}
}

// Release releases an acquired concurrency slot.
func (l *Limiter) Release() {
	select {
	case <-l.sem:
	default:
	}
}

// Take coordinates concurrency reservation, cool-down wait, and rate limit acquisition.
func (l *Limiter) Take(ctx context.Context) (func(), error) {
	if err := l.Acquire(ctx); err != nil {
		return nil, err
	}

	if err := l.Wait(ctx); err != nil {
		l.Release()
		return nil, err
	}

	return l.Release, nil
}
