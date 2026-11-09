package parse

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/NotRealShanks/badgeboard/internal/config"
	"github.com/NotRealShanks/badgeboard/internal/model"
)

func loadFixture(t *testing.T, filename string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read test fixture %s: %v", filename, err)
	}
	return data
}

// TestPhase1Gate_RealSample verifies Phase 1 Gate:
// The parser returns exactly 10 badges for the real sample profile fixture.
func TestPhase1Gate_RealSample(t *testing.T) {
	html := loadFixture(t, "sample_profile.html")

	student := model.Student{
		ProfileURL: "https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7",
		UUID:       "be556f7b-ab4b-4be7-82fb-9be45202ddd7",
	}

	result := ParseResult(html, student, config.DefaultConfig())

	if result.Status != model.StatusComplete {
		t.Fatalf("expected status %v, got %v (err: %s)", model.StatusComplete, result.Status, result.Error)
	}

	if result.BadgeCount != 10 {
		t.Fatalf("GATE FAILURE: expected exactly 10 badges, got %d", result.BadgeCount)
	}

	if len(result.Badges) != 10 {
		t.Fatalf("expected 10 badges in Badges slice, got %d", len(result.Badges))
	}

	if result.DisplayName != "Sample Learner" {
		t.Errorf("Sample Learner", "Sample Learner", result.DisplayName)
	}

	// When student name was empty, DisplayName should populate student name
	if result.Student.Name != "Sample Learner" {
		Sample Learner, got %q", "Sample Learner", result.Student.Name)
	}

	if result.League != "Silver League" {
		t.Errorf("expected League %q, got %q", "Silver League", result.League)
	}

	if result.Points != 339 {
		t.Errorf("expected Points 339, got %d", result.Points)
	}

	if result.LastBadgeEarnedAt == nil {
		t.Fatalf("expected LastBadgeEarnedAt to be populated")
	}

	expectedLastYear := 2026
	if result.LastBadgeEarnedAt.Year() != expectedLastYear {
		t.Errorf("expected last badge year %d, got %d", expectedLastYear, result.LastBadgeEarnedAt.Year())
	}

	// Verify all badges have titles, IDs, and valid earned dates
	for i, badge := range result.Badges {
		if badge.ID == "" {
			t.Errorf("badge %d missing ID", i)
		}
		if badge.Title == "" {
			t.Errorf("badge %d missing Title", i)
		}
		if badge.EarnedAt.IsZero() {
			t.Errorf("badge %d missing EarnedAt parsed date (raw: %q)", i, badge.RawEarned)
		}
	}
}

func TestParse_ProvisionalFixtures(t *testing.T) {
	tests := []struct {
		name          string
		fixtureFile   string
		expectStatus  model.Status
		expectCount   int
		expectDisplay string
	}{
		{
			name:          "Provisional zero badges (public profile, 0 badges)",
			fixtureFile:   "provisional_zero_badges.html",
			expectStatus:  model.StatusNoBadges,
			expectCount:   0,
			expectDisplay: "Zero Badges Student",
		},
		{
			name:          "Provisional partial badges (4 badges)",
			fixtureFile:   "provisional_partial_badges.html",
			expectStatus:  model.StatusInProgress,
			expectCount:   4,
			expectDisplay: "Partial Student",
		},
		{
			name:          "Provisional over 10 badges (12 badges)",
			fixtureFile:   "provisional_over_10_badges.html",
			expectStatus:  model.StatusComplete,
			expectCount:   12,
			expectDisplay: "Overachiever Student",
		},
		{
			name:          "Provisional private or not found (must NEVER be reported as zero badges)",
			fixtureFile:   "provisional_private_or_not_found.html",
			expectStatus:  model.StatusPrivateOrNotFound,
			expectCount:   0,
			expectDisplay: "",
		},
		{
			name:          "Malformed HTML / 502 error",
			fixtureFile:   "malformed.html",
			expectStatus:  model.StatusPrivateOrNotFound,
			expectCount:   0,
			expectDisplay: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			html := loadFixture(t, tc.fixtureFile)
			student := model.Student{UUID: "test-uuid"}
			res := ParseResult(html, student, config.DefaultConfig())

			if res.Status != tc.expectStatus {
				t.Errorf("expected status %v, got %v", tc.expectStatus, res.Status)
			}
			if res.BadgeCount != tc.expectCount {
				t.Errorf("expected badge count %d, got %d", tc.expectCount, res.BadgeCount)
			}
			if tc.expectDisplay != "" && res.DisplayName != tc.expectDisplay {
				t.Errorf("Sample Learner", tc.expectDisplay, res.DisplayName)
			}
		})
	}
}

func TestParse_EmptyInput(t *testing.T) {
	student := model.Student{UUID: "empty"}
	res := ParseResult([]byte("   "), student, config.DefaultConfig())
	if res.Status != model.StatusParseError {
		t.Errorf("expected StatusParseError for empty HTML, got %v", res.Status)
	}
}

func TestEvaluate_WithParseError(t *testing.T) {
	student := model.Student{UUID: "err-student"}
	res := Evaluate(nil, student, nil, errors.New("underlying parse failure"))
	if res.Status != model.StatusParseError {
		t.Errorf("expected StatusParseError, got %v", res.Status)
	}
	if res.Error != "underlying parse failure" {
		t.Errorf("expected error message preserved, got %q", res.Error)
	}
}

func TestParse_RelativeBadgeURLAndFallbackAria(t *testing.T) {
	html := `
<!DOCTYPE html>
<html>
<head><title>Test User | Google Skills</title></head>
<body>
  <div class="text--center">
    <h1>Test User</h1>
    <div class="profile-badges">
      <div class="profile-badge">
        <a href="/public_profiles/1111/badges/99999"></a>
        <ql-button ariaLabel="Learn more about Cloud Architecture Badge"></ql-button>
        <span>Earned 2026-10-15</span>
      </div>
    </div>
  </div>
</body>
</html>`
	student := model.Student{UUID: "1111"}
	res := ParseResult([]byte(html), student, nil)

	if res.Status != model.StatusInProgress {
		t.Errorf("expected InProgress with 1 badge and nil config, got %v", res.Status)
	}
	if res.BadgeCount != 1 {
		t.Fatalf("expected 1 badge, got %d", res.BadgeCount)
	}
	if res.Badges[0].URL != "https://www.skills.google/public_profiles/1111/badges/99999" {
		t.Errorf("unexpected normalized URL: %s", res.Badges[0].URL)
	}
	if res.Badges[0].Title != "Cloud Architecture Badge" {
		t.Errorf("unexpected fallback aria title: %s", res.Badges[0].Title)
	}
}

func TestParse_ConfigFiltering(t *testing.T) {
	html := loadFixture(t, "sample_profile.html")
	student := model.Student{UUID: "be556f7b-ab4b-4be7-82fb-9be45202ddd7"}

	// Restrict to only 1 specific badge
	cfg := &config.Config{
		Goal: 1,
		RequiredBadges: []string{
			"Explore Generative AI in Agent Platform",
		},
	}

	res := ParseResult(html, student, cfg)
	if res.BadgeCount != 1 {
		t.Fatalf("expected 1 filtered badge, got %d", res.BadgeCount)
	}
	if res.Badges[0].Title != "Explore Generative AI in Agent Platform" {
		t.Errorf("unexpected badge title: %s", res.Badges[0].Title)
	}
	if res.Status != model.StatusComplete {
		t.Errorf("expected StatusComplete with goal 1, got %v", res.Status)
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Nov  3, 2026 EST", "2026-11-03"},
		{"Nov 15, 2026 EST", "2026-11-15"},
		{"2026-10-12", "2026-10-12"},
		{"unparseable-date", ""},
	}

	for _, tc := range tests {
		got := parseDate(tc.input)
		if tc.want == "" {
			if !got.IsZero() {
				t.Errorf("parseDate(%q) expected zero time, got %v", tc.input, got)
			}
		} else {
			if got.Format("2006-01-02") != tc.want {
				t.Errorf("parseDate(%q) = %s, want %s", tc.input, got.Format("2006-01-02"), tc.want)
			}
		}
	}
}
