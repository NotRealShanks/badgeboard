package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Status represents the processing status of a student profile.
type Status string

const (
	// StatusComplete indicates the student has completed the target number of badges.
	StatusComplete Status = "complete"
	// StatusInProgress indicates the student has earned badges but fewer than the target goal.
	StatusInProgress Status = "in_progress"
	// StatusNoBadges indicates the profile loaded, has positive profile markers, and has 0 badges.
	StatusNoBadges Status = "no_badges"
	// StatusPrivateOrNotFound indicates the profile is inaccessible, private, redirected to login, or 404.
	StatusPrivateOrNotFound Status = "private_or_not_found"
	// StatusInvalidURL indicates the provided profile URL does not match valid skills.google profile format.
	StatusInvalidURL Status = "invalid_url"
	// StatusRateLimited indicates the request was throttled (HTTP 429).
	StatusRateLimited Status = "rate_limited"
	// StatusNetworkError indicates a connection, timeout, DNS, or transport failure.
	StatusNetworkError Status = "network_error"
	// StatusParseError indicates HTML parsing failed or returned malformed structure.
	StatusParseError Status = "parse_error"
)

// String returns the string representation of Status.
func (s Status) String() string {
	return string(s)
}

// MarshalJSON marshals Status into quoted JSON string.
func (s Status) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// UnmarshalJSON unmarshals a JSON string into Status.
func (s *Status) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	*s = Status(str)
	return nil
}

// EvaluateStatus determines the profile status based on counted eligible badges, campaign goal,
// total raw badges on profile, and private state.
func EvaluateStatus(countedBadges int, goal int, totalBadges int, isPrivate bool) Status {
	if isPrivate {
		return StatusPrivateOrNotFound
	}
	if totalBadges == 0 {
		return StatusNoBadges
	}
	if goal <= 0 {
		goal = 10
	}
	if countedBadges >= goal {
		return StatusComplete
	}
	if countedBadges > 0 {
		return StatusInProgress
	}
	return StatusInProgress
}

// Student represents a participant loaded from a roster.
type Student struct {
	ProfileURL string `json:"profile_url,omitempty"`
	UUID       string `json:"uuid,omitempty"`
	Name       string `json:"name,omitempty"`
	Email      string `json:"email,omitempty"`
	Team       string `json:"team,omitempty"`
	RowNumber  int    `json:"row_number,omitempty"`
}

// BadgeRecord represents a single earned skill badge.
type BadgeRecord struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	EarnedAt  time.Time `json:"earned_at"`
	RawEarned string    `json:"raw_earned"`
	URL       string    `json:"url,omitempty"`
	ImageURL  string    `json:"image_url,omitempty"`
}

// TimePtr returns a pointer to the given time.Time (or nil if zero).
func TimePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// ProfileResult represents the complete tracking evaluation for a single student.
type ProfileResult struct {
	Student           Student       `json:"student"`
	Status            Status        `json:"status"`
	DisplayName       string        `json:"display_name,omitempty"`
	Pseudonym         string        `json:"pseudonym,omitempty"`
	League            string        `json:"league,omitempty"`
	Points            int           `json:"points,omitempty"`
	Badges            []BadgeRecord `json:"badges,omitempty"`
	TotalBadges       int           `json:"total_badges"`
	CountedBadges     int           `json:"counted_badges"`
	LastBadgeDate     string        `json:"last_badge_date,omitempty"`
	LastBadgeEarnedAt *time.Time    `json:"last_badge_earned_at,omitempty"`
	Error             string        `json:"error,omitempty"`
	FetchedAt         *time.Time    `json:"fetched_at,omitempty"`
	FromCache         bool          `json:"from_cache,omitempty"`
}

// FallbackProfileLabel creates an unambiguous fallback label of the form "Profile (be55…)"
// built from the first 4 characters of the UUID.
func FallbackProfileLabel(uuid string) string {
	clean := strings.TrimSpace(uuid)
	prefix := "????"
	if len(clean) >= 4 {
		prefix = clean[:4]
	} else if clean != "" {
		prefix = clean
	}
	return fmt.Sprintf("Profile (%s…)", prefix)
}

