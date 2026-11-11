package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/fetch"
	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/NotRealShanks/badgeboard/internal/parse"
	"github.com/NotRealShanks/badgeboard/internal/roster"
	"github.com/NotRealShanks/badgeboard/internal/tracker"
)

// Runner manages the background execution of an evaluation run.
type Runner struct {
	serverTracker *tracker.Tracker
	store         *Store
}

// NewRunner initializes a runner bound to the server's shared tracker engine.
func NewRunner(serverTracker *tracker.Tracker, store *Store) *Runner {
	return &Runner{
		serverTracker: serverTracker,
		store:         store,
	}
}

// ExecuteRun executes the evaluation for a run in a background goroutine.
func (rn *Runner) ExecuteRun(ctx context.Context, run *Run, rst *roster.Roster) {
	run.mu.Lock()
	run.Status = StatusRunning
	run.mu.Unlock()

	startTime := time.Now()
	run.Broadcast("run_started", fmt.Sprintf(`{"run_id":"%s","total":%d}`, run.ID, len(rst.Students)))

	ctx, cancel := context.WithCancel(ctx)
	run.SetCancel(cancel)
	defer cancel()

	var cacheHits int
	var networkReqs int
	var retries int
	var pauseDuration time.Duration

	// Stream progress and row results using the server's tracker
	results, err := rn.serverTracker.Track(ctx, rst, func(current, total int, res model.ProfileResult) {
		if res.FromCache {
			cacheHits++
		} else {
			networkReqs++
		}

		elapsed := time.Since(startTime)
		netReqs := current - cacheHits
		netRate := 0.0
		if elapsed.Seconds() > 0 && netReqs > 0 {
			netRate = float64(netReqs) / elapsed.Seconds()
		}
		remaining := total - current
		etaDur := time.Duration(0)
		if remaining > 0 && netRate > 0 {
			etaDur = time.Duration(float64(remaining)/netRate) * time.Second
		}

		// Sanitize result according to run privacy settings
		sanitizedRow := run.SanitizeResult(res)

		// Broadcast row_result event
		rowBytes, _ := json.Marshal(sanitizedRow)
		run.Broadcast("row_result", string(rowBytes))

		// Broadcast progress event
		progressStats := ProgressStats{
			Done:     current,
			Total:    total,
			Rate:     netRate,
			Cached:   cacheHits,
			ETA:      fmt.Sprintf("%ds", int(etaDur.Seconds())),
			IsPaused: false,
		}
		progBytes, _ := json.Marshal(progressStats)
		run.Broadcast("progress", string(progBytes))
	})

	run.mu.Lock()
	defer run.mu.Unlock()

	if errors.Is(ctx.Err(), context.Canceled) {
		run.Status = StatusCancelled
		run.Broadcast("run_cancelled", `{"reason":"cancelled by operator"}`)
		return
	}

	if err != nil && len(results) == 0 {
		run.Status = StatusError
		errJson, _ := json.Marshal(map[string]string{"error": err.Error()})
		run.Broadcast("error", string(errJson))
		return
	}

	// Partition results into completed/in-progress vs unreachable
	run.Results = make([]model.ProfileResult, 0)
	run.Unreachable = make([]model.ProfileResult, 0)

	var completeCount, inProgressCount, noBadgesCount, unreachableCount int

	for _, r := range results {
		sanitized := run.SanitizeResult(r)
		switch r.Status {
		case model.StatusComplete:
			completeCount++
			run.Results = append(run.Results, sanitized)
		case model.StatusInProgress:
			inProgressCount++
			run.Results = append(run.Results, sanitized)
		case model.StatusNoBadges:
			noBadgesCount++
			run.Results = append(run.Results, sanitized)
		case model.StatusPrivateOrNotFound, model.StatusNetworkError, model.StatusRateLimited, model.StatusParseError:
			unreachableCount++
			run.Unreachable = append(run.Unreachable, sanitized)
		default:
			run.Results = append(run.Results, sanitized)
		}
	}

	duration := time.Since(startTime)
	run.Summary = RunSummary{
		Goal:                  run.Settings.Goal,
		TotalStudents:         len(rst.Students),
		CompleteCount:         completeCount,
		InProgressCount:       inProgressCount,
		NoBadgesCount:         noBadgesCount,
		UnreachableCount:      unreachableCount,
		NetworkRequests:       networkReqs,
		CacheHits:             cacheHits,
		Retries:               retries,
		RateLimitPauseSeconds: pauseDuration.Seconds(),
		DurationSeconds:       duration.Seconds(),
		BadRowsCount:          len(rst.BadRows),
		Anonymized:            run.Settings.Anonymize,
	}

	run.Status = StatusCompleted
	snapshotBytes, _ := json.Marshal(run)
	run.Broadcast("run_complete", string(snapshotBytes))
}

// RetryUnreachable re-evaluates only transient errors (network error, rate limited) for an existing run.
func (rn *Runner) RetryUnreachable(ctx context.Context, run *Run) error {
	run.mu.RLock()
	var retryStudents []model.Student
	for _, u := range run.Unreachable {
		if u.Status == model.StatusNetworkError || u.Status == model.StatusRateLimited {
			retryStudents = append(retryStudents, u.Student)
		}
	}
	run.mu.RUnlock()

	if len(retryStudents) == 0 {
		return errors.New("no retryable profiles found (private or 404 profiles are not retried)")
	}

	retryRoster := &roster.Roster{
		Students: retryStudents,
	}

	// Execute re-evaluation in background
	go rn.ExecuteRun(ctx, run, retryRoster)
	return nil
}

// CheckSingleProfile fetches and evaluates a single URL using the shared fetcher.
func (rn *Runner) CheckSingleProfile(ctx context.Context, rawURL string, goal int) (model.ProfileResult, error) {
	canonicalURL, uuid, err := roster.NormalizeURL(rawURL)
	if err != nil {
		return model.ProfileResult{}, fmt.Errorf("invalid profile URL %q: %w", rawURL, err)
	}

	f := fetch.New()
	html, err := f.Fetch(ctx, canonicalURL)
	if err != nil {
		if errors.Is(err, fetch.ErrNotFoundOrPrivate) {
			return model.ProfileResult{
				Student: model.Student{
					ProfileURL: canonicalURL,
					UUID:       uuid,
				},
				Status: model.StatusPrivateOrNotFound,
			}, nil
		}
		return model.ProfileResult{}, fmt.Errorf("fetching profile: %w", err)
	}

	data, err := parse.Parse(html)
	if err != nil {
		return model.ProfileResult{}, fmt.Errorf("parsing profile: %w", err)
	}

	status := model.EvaluateStatus(len(data.Badges), goal, len(data.Badges), data.IsPrivate)
	displayName := data.DisplayName
	if strings.TrimSpace(displayName) == "" {
		displayName = "Anonymous"
	}

	return model.ProfileResult{
		Student: model.Student{
			ProfileURL: canonicalURL,
			UUID:       uuid,
			Name:       displayName,
		},
		Status:        status,
		DisplayName:   displayName,
		League:        data.League,
		Points:        data.Points,
		TotalBadges:   len(data.Badges),
		CountedBadges: len(data.Badges),
		Badges:        data.Badges,
	}, nil
}
