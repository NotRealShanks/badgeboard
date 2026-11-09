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

// Limiter coordinates request rates and concurrency bounds.
type Limiter struct {
	rps         float64
	concurrency int
	clock       Clock
	rateLimiter *rate.Limiter
	sem         chan struct{}
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

// Wait blocks until the rate limiter permits an action according to RPS,
// respecting context cancellation.
func (l *Limiter) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	l.mu.Lock()
	r := l.rateLimiter.ReserveN(l.clock.Now(), 1)
	if !r.OK() {
		l.mu.Unlock()
		return fmt.Errorf("rate limiter burst capacity exceeded")
	}
	delay := r.DelayFrom(l.clock.Now())
	l.mu.Unlock()

	if delay <= 0 {
		return nil
	}

	// For custom test clocks, dispatch via clock.Sleep directly if context not canceled
	if _, isReal := l.clock.(realClock); !isReal {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.clock.Sleep(delay)
		return ctx.Err()
	}

	timer := time.NewTimer(delay)
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

// Take coordinates both rate limit acquisition and concurrency reservation.
// It returns a release closure to be called when the task completes.
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
