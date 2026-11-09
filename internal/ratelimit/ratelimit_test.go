package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu          sync.Mutex
	currentTime time.Time
	sleepCalls  []time.Duration
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{currentTime: start}
}

func (fc *fakeClock) Now() time.Time {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.currentTime
}

func (fc *fakeClock) Sleep(d time.Duration) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.sleepCalls = append(fc.sleepCalls, d)
	fc.currentTime = fc.currentTime.Add(d)
}

func (fc *fakeClock) totalSlept() time.Duration {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	var total time.Duration
	for _, s := range fc.sleepCalls {
		total += s
	}
	return total
}

func TestLimiter_DefaultsAndOptions(t *testing.T) {
	lim := New(0, 0, WithClock(nil))
	if lim.rps != DefaultRPS {
		t.Errorf("expected default RPS %f, got %f", DefaultRPS, lim.rps)
	}
	if lim.concurrency != DefaultConcurrency {
		t.Errorf("expected default concurrency %d, got %d", DefaultConcurrency, lim.concurrency)
	}

	// Double release is a safe no-op
	lim.Release()
}

func TestLimiter_RateWaitDeterministic(t *testing.T) {
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)

	// 2 requests per second (500ms delay between consecutive requests)
	lim := New(2.0, 2, WithClock(clock))

	ctx := context.Background()

	// First request: immediate (burst 1 available)
	if err := lim.Wait(ctx); err != nil {
		t.Fatalf("first Wait failed: %v", err)
	}
	if len(clock.sleepCalls) != 0 {
		t.Errorf("expected 0 sleeps for first request, got %d", len(clock.sleepCalls))
	}

	// Second request: should wait 500ms
	if err := lim.Wait(ctx); err != nil {
		t.Fatalf("second Wait failed: %v", err)
	}
	if len(clock.sleepCalls) != 1 {
		t.Fatalf("expected 1 sleep for second request, got %d", len(clock.sleepCalls))
	}
	if clock.sleepCalls[0] != 500*time.Millisecond {
		t.Errorf("expected sleep duration 500ms, got %v", clock.sleepCalls[0])
	}

	// Total elapsed virtual time should be 500ms
	if clock.totalSlept() != 500*time.Millisecond {
		t.Errorf("expected 500ms total virtual sleep, got %v", clock.totalSlept())
	}

	// Wait with canceled context on custom clock
	cancCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := lim.Wait(cancCtx); err == nil {
		t.Errorf("expected error on canceled context with custom clock")
	}
}

func TestLimiter_ConcurrencyBounded(t *testing.T) {
	maxConcurrency := 2
	lim := New(100.0, maxConcurrency)
	ctx := context.Background()

	var active int32
	var maxObserved int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := lim.Take(ctx)
			if err != nil {
				return
			}
			cur := atomic.AddInt32(&active, 1)
			for {
				oldMax := atomic.LoadInt32(&maxObserved)
				if cur <= oldMax || atomic.CompareAndSwapInt32(&maxObserved, oldMax, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			release()
		}()
	}

	wg.Wait()

	if maxObserved > int32(maxConcurrency) {
		t.Errorf("concurrency exceeded limit: observed %d, limit %d", maxObserved, maxConcurrency)
	}
}

func TestLimiter_ContextCancellation(t *testing.T) {
	lim := New(1.0, 1)

	// Consume the single concurrency slot
	ctx := context.Background()
	release, err := lim.Take(ctx)
	if err != nil {
		t.Fatalf("initial Take failed: %v", err)
	}
	defer release()

	// Canceled context on Take
	cancCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = lim.Take(cancCtx)
	if err == nil {
		t.Errorf("expected error with canceled context, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// Canceled context on Wait
	err = lim.Wait(cancCtx)
	if err == nil {
		t.Errorf("expected error on Wait with canceled context")
	}

	// Wait with real clock timer cancellation
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer waitCancel()
	_ = lim.Wait(context.Background())
	err = lim.Wait(waitCtx)
	if err == nil {
		t.Errorf("expected timeout on real clock wait")
	}
}
