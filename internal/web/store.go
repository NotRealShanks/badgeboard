package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
)

// RunStatus represents the current lifecycle state of an evaluation run.
type RunStatus string

const (
	StatusStarting  RunStatus = "starting"
	StatusRunning   RunStatus = "running"
	StatusCompleted RunStatus = "completed"
	StatusCancelled RunStatus = "cancelled"
	StatusError     RunStatus = "error"
)

// RunSettings holds operator options for an evaluation run.
type RunSettings struct {
	Goal          int     `json:"goal"`
	RPS           float64 `json:"rps"`
	Concurrency   int     `json:"concurrency"`
	Anonymize     bool    `json:"anonymize"`
	IncludeBadges bool    `json:"include_badges"`
	BypassCache   bool    `json:"bypass_cache"`
}

// SSEEvent represents a single Server-Sent Event message with a monotonic ID.
type SSEEvent struct {
	ID    int    `json:"id"`
	Event string `json:"event"`
	Data  string `json:"data"`
}

// Run represents an active or completed evaluation session.
type Run struct {
	ID        string      `json:"id"`
	Status    RunStatus   `json:"status"`
	CreatedAt time.Time   `json:"created_at"`
	ExpiresAt time.Time   `json:"expires_at"`
	Settings  RunSettings `json:"settings"`

	Salt []byte `json:"-"` // Cryptographic random salt for HMAC-SHA256 anonymization

	Summary     RunSummary            `json:"summary"`
	Results     []model.ProfileResult `json:"results"`
	Unreachable []model.ProfileResult `json:"unreachable"`

	// Event broadcasting
	mu          sync.RWMutex
	eventSeq    int
	eventLog    []SSEEvent
	subscribers map[chan SSEEvent]struct{}
	cancelFunc  func()
}

// RunSummary represents aggregated statistics for a run.
type RunSummary struct {
	Goal                  int     `json:"goal"`
	TotalStudents         int     `json:"total_students"`
	CompleteCount         int     `json:"complete_count"`
	InProgressCount       int     `json:"in_progress_count"`
	NoBadgesCount         int     `json:"no_badges_count"`
	UnreachableCount      int     `json:"unreachable_count"`
	NetworkRequests       int     `json:"network_requests"`
	CacheHits             int     `json:"cache_hits"`
	Retries               int     `json:"retries"`
	RateLimitPauseSeconds float64 `json:"rate_limit_pause_seconds"`
	DurationSeconds       float64 `json:"duration_seconds"`
	BadRowsCount          int     `json:"bad_rows_count"`
	Anonymized            bool    `json:"anonymized"`
}

// ProgressStats carries live progress metrics emitted via SSE.
type ProgressStats struct {
	Done     int     `json:"done"`
	Total    int     `json:"total"`
	Rate     float64 `json:"rate"`
	Cached   int     `json:"cached"`
	ETA      string  `json:"eta"`
	IsPaused bool    `json:"is_paused"`
	PauseETA int     `json:"pause_eta,omitempty"`
}

// Store is an in-memory thread-safe store for all evaluation runs.
type Store struct {
	mu      sync.RWMutex
	runs    map[string]*Run
	maxRuns int
	ttl     time.Duration
}

// NewStore initializes a new Store and starts the background janitor.
func NewStore(maxRuns int, ttl time.Duration) *Store {
	if maxRuns <= 0 {
		maxRuns = 3
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	s := &Store{
		runs:    make(map[string]*Run),
		maxRuns: maxRuns,
		ttl:     ttl,
	}

	go s.janitorLoop(1 * time.Minute)
	return s
}

// CreateRun initializes a new Run with a random unguessable ID and secret salt.
func (s *Store) CreateRun(settings RunSettings, totalCount int) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check concurrent active runs limit
	activeCount := 0
	for _, r := range s.runs {
		if r.Status == StatusRunning || r.Status == StatusStarting {
			activeCount++
		}
	}
	if activeCount >= s.maxRuns {
		return nil, fmt.Errorf("server busy: maximum active runs reached (%d/%d)", activeCount, s.maxRuns)
	}

	// 16 random bytes = 32 hex chars unguessable ID
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generating run id: %w", err)
	}
	runID := hex.EncodeToString(idBytes)

	// 32 random bytes secret salt for HMAC-SHA256 pseudonymization
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generating secret salt: %w", err)
	}

	now := time.Now()
	run := &Run{
		ID:          runID,
		Status:      StatusStarting,
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.ttl),
		Settings:    settings,
		Salt:        salt,
		Results:     make([]model.ProfileResult, 0),
		Unreachable: make([]model.ProfileResult, 0),
		Summary: RunSummary{
			Goal:          settings.Goal,
			TotalStudents: totalCount,
			Anonymized:    settings.Anonymize,
		},
		subscribers: make(map[chan SSEEvent]struct{}),
		eventLog:    make([]SSEEvent, 0),
	}

	s.runs[runID] = run
	return run, nil
}

// Get returns the Run by ID if it exists and has not expired.
func (s *Store) Get(runID string) (*Run, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.runs[runID]
	if !ok || time.Now().After(r.ExpiresAt) {
		return nil, false
	}
	return r, true
}

func (s *Store) janitorLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		s.purgeExpired()
	}
}

func (s *Store) purgeExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, r := range s.runs {
		if now.After(r.ExpiresAt) {
			delete(s.runs, id)
		}
	}
}

// RLx helper for Read lock
func (s *Store) mu_RLock() {
	s.mu.RLock()
}

// Broadcast sends an SSE event to all active subscribers and appends it to eventLog.
func (r *Run) Broadcast(event string, data string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.eventSeq++
	e := SSEEvent{
		ID:    r.eventSeq,
		Event: event,
		Data:  data,
	}
	r.eventLog = append(r.eventLog, e)

	for ch := range r.subscribers {
		select {
		case ch <- e:
		default:
			// Non-blocking drop if client is slow
		}
	}
}

// Subscribe attaches a new event listener channel, playing back any events missed since lastEventID.
func (r *Run) Subscribe(lastEventID int) (chan SSEEvent, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ch := make(chan SSEEvent, 64)
	r.subscribers[ch] = struct{}{}

	// Replay missed events
	if lastEventID > 0 {
		for _, e := range r.eventLog {
			if e.ID > lastEventID {
				select {
				case ch <- e:
				default:
				}
			}
		}
	}

	unsubscribe := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.subscribers, ch)
		close(ch)
	}

	return ch, unsubscribe
}

// SetCancel stores the cancellation function.
func (r *Run) SetCancel(cancel func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelFunc = cancel
}

// Cancel calls the cancel function if active and transitions status.
func (r *Run) Cancel() bool {
	r.mu.Lock()
	if r.Status == StatusRunning || r.Status == StatusStarting {
		if r.cancelFunc != nil {
			r.cancelFunc()
		}
		r.Status = StatusCancelled
		r.mu.Unlock()
		r.Broadcast("run_cancelled", `{"reason":"cancelled by operator"}`)
		return true
	}
	r.mu.Unlock()
	return false
}

// DerivePseudonym creates a secure, non-reversible, non-guessable HMAC-SHA256 pseudonym.
func (r *Run) DerivePseudonym(uuid string) string {
	mac := hmac.New(sha256.New, r.Salt)
	mac.Write([]byte(strings.ToLower(strings.TrimSpace(uuid))))
	sum := mac.Sum(nil)
	// Formatted as Learner-XXXX using first 4 hex chars
	return fmt.Sprintf("Learner-%X", sum[:2])
}

// SanitizeForPublic redacts PII before output when Anonymize is active.
func (r *Run) SanitizeResult(res model.ProfileResult) model.ProfileResult {
	if !r.Settings.Anonymize {
		return res
	}

	sanitized := res
	pseudo := r.DerivePseudonym(res.Student.UUID)
	sanitized.DisplayName = pseudo
	sanitized.Pseudonym = pseudo
	sanitized.Student.Name = pseudo
	sanitized.Student.ProfileURL = ""
	sanitized.Student.UUID = ""

	if !r.Settings.IncludeBadges {
		// Public Tier: strip leagues, points, individual badge list, and exact timestamps
		sanitized.League = ""
		sanitized.Points = 0
		sanitized.Badges = nil
	} else {
		// Include Badges Tier: keep titles and dates, but strip URLs
		cleanBadges := make([]model.BadgeRecord, len(res.Badges))
		for i, b := range res.Badges {
			cleanBadges[i] = model.BadgeRecord{
				ID:       fmt.Sprintf("b-%d", i+1),
				Title:    b.Title,
				EarnedAt: b.EarnedAt,
			}
		}
		sanitized.Badges = cleanBadges
	}

	return sanitized
}
