package model

import (
	"encoding/json"
	"time"
)

// Status represents the processing status of a student profile.
type Status string

const (
	// StatusComplete indicates the student has completed the target number of badges (e.g. 10).
	StatusComplete Status = "complete"
	// StatusInProgress indicates the student has earned badges but fewer than the target goal.
	StatusInProgress Status = "in_progress"
	// StatusNoBadges indicates the profile loaded and is public, but 0 badges have been earned.
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

// Student represents a participant loaded from a roster.
type Student struct {
	ProfileURL string `json:"profile_url"`
	UUID       string `json:"uuid"`
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
	URL       string    `json:"url"`
	ImageURL  string    `json:"image_url,omitempty"`
}

// ProfileResult represents the complete tracking evaluation for a single student.
type ProfileResult struct {
	Student           Student       `json:"student"`
	Status            Status        `json:"status"`
	DisplayName       string        `json:"display_name,omitempty"`
	League            string        `json:"league,omitempty"`
	Points            int           `json:"points,omitempty"`
	Badges            []BadgeRecord `json:"badges,omitempty"`
	BadgeCount        int           `json:"badge_count"`
	LastBadgeEarnedAt *time.Time    `json:"last_badge_earned_at,omitempty"`
	Error             string        `json:"error,omitempty"`
	FetchedAt         time.Time     `json:"fetched_at"`
	FromCache         bool          `json:"from_cache"`
}
