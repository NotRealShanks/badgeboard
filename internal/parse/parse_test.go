package parse

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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

	data, err := Parse(html)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	result := Evaluate(data, student, config.DefaultGoal, nil, nil)

	if result.Status != model.StatusComplete {
		t.Fatalf("expected status %v, got %v (err: %s)", model.StatusComplete, result.Status, result.Error)
	}

	if result.TotalBadges != 10 {
		t.Fatalf("GATE FAILURE: expected exactly 10 total badges, got %d", result.TotalBadges)
	}
	if result.CountedBadges != 10 {
		t.Fatalf("GATE FAILURE: expected exactly 10 counted badges, got %d", result.CountedBadges)
	}
	if result.BadgeCount != 10 {
		t.Fatalf("GATE FAILURE: expected BadgeCount alias 10, got %d", result.BadgeCount)
	}

	if len(result.Badges) != 10 {
		t.Fatalf("expected 10 badges in Badges slice, got %d", len(result.Badges))
	}

	if result.DisplayName != "Sample Learner" {
		t.Errorf("Sample Learner", "Sample Learner", result.DisplayName)
	}

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
		expectTotal   int
		expectCounted int
		expectDisplay string
	}{
		{
			name:          "Provisional zero badges (public profile, 0 badges)",
			fixtureFile:   "provisional_zero_badges.html",
			expectStatus:  model.StatusNoBadges,
			expectTotal:   0,
			expectCounted: 0,
			expectDisplay: "Zero Badges Student",
		},
		{
			name:          "Provisional partial badges (4 badges)",
			fixtureFile:   "provisional_partial_badges.html",
			expectStatus:  model.StatusInProgress,
			expectTotal:   4,
			expectCounted: 4,
			expectDisplay: "Partial Student",
		},
		{
			name:          "Provisional over 10 badges (12 badges)",
			fixtureFile:   "provisional_over_10_badges.html",
			expectStatus:  model.StatusComplete,
			expectTotal:   12,
			expectCounted: 12,
			expectDisplay: "Overachiever Student",
		},
		{
			name:          "Provisional private or not found (must NEVER be reported as zero badges)",
			fixtureFile:   "provisional_private_or_not_found.html",
			expectStatus:  model.StatusPrivateOrNotFound,
			expectTotal:   0,
			expectCounted: 0,
			expectDisplay: "",
		},
		{
			name:          "Malformed HTML / 502 error must resolve to ParseError, not PrivateOrNotFound",
			fixtureFile:   "malformed.html",
			expectStatus:  model.StatusParseError,
			expectTotal:   0,
			expectCounted: 0,
			expectDisplay: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			html := loadFixture(t, tc.fixtureFile)
			student := model.Student{UUID: "test-uuid"}
			data, err := Parse(html)
			res := Evaluate(data, student, config.DefaultGoal, nil, err)

			if res.Status != tc.expectStatus {
				t.Errorf("expected status %v, got %v (err: %s)", tc.expectStatus, res.Status, res.Error)
			}
			if res.TotalBadges != tc.expectTotal {
				t.Errorf("expected total badges %d, got %d", tc.expectTotal, res.TotalBadges)
			}
			if res.CountedBadges != tc.expectCounted {
				t.Errorf("expected counted badges %d, got %d", tc.expectCounted, res.CountedBadges)
			}
			if tc.expectDisplay != "" && res.DisplayName != tc.expectDisplay {
				t.Errorf("Sample Learner", tc.expectDisplay, res.DisplayName)
			}
		})
	}
}

func TestNoBadges_RequiresPositiveProfileMarkers(t *testing.T) {
	// Case A: h1 present with no other markers and zero badges -> must be ParseError, NEVER NoBadges
	htmlWithoutMarkers := `
<!DOCTYPE html>
<html>
<head><title>Some Random Page</title></head>
<body>
  <h1>John Doe</h1>
  <p>Just a random page with zero profile markers.</p>
</body>
</html>`
	data, err := Parse([]byte(htmlWithoutMarkers))
	res := Evaluate(data, model.Student{UUID: "123"}, config.DefaultGoal, nil, err)
	if res.Status != model.StatusParseError {
		t.Errorf("expected StatusParseError for h1 with no markers, got %v", res.Status)
	}

	// Case B: h1 present with explicit private profile message -> must be PrivateOrNotFound
	htmlPrivateWithMessage := `
<!DOCTYPE html>
<html>
<head><title>User Profile | Google Skills</title></head>
<body>
  <h1>Private User</h1>
  <div class="notice">This profile is private.</div>
</body>
</html>`
	dataPriv, errPriv := Parse([]byte(htmlPrivateWithMessage))
	resPriv := Evaluate(dataPriv, model.Student{UUID: "123"}, config.DefaultGoal, nil, errPriv)
	if resPriv.Status != model.StatusPrivateOrNotFound {
		t.Errorf("expected StatusPrivateOrNotFound for h1 with private message, got %v", resPriv.Status)
	}
}

func TestGoalChange_DoesNotTouchParser(t *testing.T) {
	html := loadFixture(t, "provisional_partial_badges.html") // 4 badges

	// Parse once: parser extracts raw facts only
	data, err := Parse(html)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(data.Badges) != 4 {
		t.Fatalf("expected 4 raw badges, got %d", len(data.Badges))
	}

	student := model.Student{UUID: "student-4"}

	// Evaluate with default goal 10 -> InProgress
	resDefault := Evaluate(data, student, 10, nil, nil)
	if resDefault.Status != model.StatusInProgress {
		t.Errorf("expected StatusInProgress with goal 10, got %v", resDefault.Status)
	}
	if resDefault.TotalBadges != 4 || resDefault.CountedBadges != 4 {
		t.Errorf("badges count mismatch: total=%d, counted=%d", resDefault.TotalBadges, resDefault.CountedBadges)
	}

	// Evaluate the EXACT SAME parsed data with goal 4 (or 3) -> Complete!
	resGoal3 := Evaluate(data, student, 3, nil, nil)
	if resGoal3.Status != model.StatusComplete {
		t.Errorf("expected StatusComplete with goal 3, got %v", resGoal3.Status)
	}
	if resGoal3.TotalBadges != 4 || resGoal3.CountedBadges != 4 {
		t.Errorf("badges count mismatch: total=%d, counted=%d", resGoal3.TotalBadges, resGoal3.CountedBadges)
	}
}

func TestDateParsing_IndependentOfMachineTZ(t *testing.T) {
	timezonesToTest := []string{"UTC", "Asia/Kolkata", "America/New_York", "Europe/London"}
	origTZ := os.Getenv("TZ")
	defer func() {
		if origTZ != "" {
			_ = os.Setenv("TZ", origTZ)
		} else {
			_ = os.Unsetenv("TZ")
		}
	}()

	dateStr := "Earned Nov  3, 2026 EST"
	nyLoc := NYLocation()

	for _, tz := range timezonesToTest {
		t.Run("TZ="+tz, func(t *testing.T) {
			_ = os.Setenv("TZ", tz)

			parsed := ParseDate(dateStr)
			if parsed.IsZero() {
				t.Fatalf("failed to parse date %q in TZ %s", dateStr, tz)
			}

			// In America/New_York calendar date
			inNY := parsed.In(nyLoc)
			year, month, day := inNY.Date()

			if year != 2026 || month != time.November || day != 3 {
				t.Errorf("in TZ=%s, expected 2026-11-03, got %04d-%02d-%02d", tz, year, month, day)
			}
		})
	}
}

func TestPaginationDetection_ReturnsParseError(t *testing.T) {
	htmlWithPagination := `
<!DOCTYPE html>
<html>
<head><title>Sample Learner | Google Skills</title></head>
<body>
  <h1>Sample Learner</h1>
  <p>Member since 2026</p>
  <div class="profile-badges">
    <a href="/public_profiles/1111/badges/101">Badge 1</a>
  </div>
  <div class="pagination">
    <a class="next_page" href="/public_profiles/1111?page=2">Next</a>
  </div>
</body>
</html>`

	data, err := Parse([]byte(htmlWithPagination))
	if err == nil {
		t.Fatalf("expected error for pagination control, got nil")
	}
	if !errors.Is(err, ErrPaginationDetected) {
		t.Errorf("expected ErrPaginationDetected, got %v", err)
	}
	res := Evaluate(data, model.Student{UUID: "1111"}, config.DefaultGoal, nil, err)
	if res.Status != model.StatusParseError {
		t.Errorf("expected StatusParseError for pagination, got %v", res.Status)
	}
}

func TestParse_EmptyInput(t *testing.T) {
	student := model.Student{UUID: "empty"}
	data, err := Parse([]byte("   "))
	res := Evaluate(data, student, config.DefaultGoal, nil, err)
	if res.Status != model.StatusParseError {
		t.Errorf("expected StatusParseError for empty HTML, got %v", res.Status)
	}
}

func TestEvaluate_WithParseError(t *testing.T) {
	student := model.Student{UUID: "err-student"}
	res := Evaluate(nil, student, 10, nil, errors.New("underlying parse failure"))
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
    <p>Member since 2026</p>
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
	data, err := Parse([]byte(html))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	res := Evaluate(data, student, 10, nil, nil)

	if res.Status != model.StatusInProgress {
		t.Errorf("expected InProgress with 1 badge and goal 10, got %v", res.Status)
	}
	if res.CountedBadges != 1 || res.TotalBadges != 1 {
		t.Fatalf("expected 1 badge, got counted=%d, total=%d", res.CountedBadges, res.TotalBadges)
	}
	if res.Badges[0].URL != "https://www.skills.google/public_profiles/1111/badges/99999" {
		t.Errorf("unexpected normalized URL: %s", res.Badges[0].URL)
	}
	if res.Badges[0].Title != "Cloud Architecture Badge" {
		t.Errorf("unexpected fallback aria title: %s", res.Badges[0].Title)
	}
}

func TestParse_BadgeFilter(t *testing.T) {
	html := loadFixture(t, "sample_profile.html")
	student := model.Student{UUID: "be556f7b-ab4b-4be7-82fb-9be45202ddd7"}

	data, err := Parse(html)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Filter to only 1 badge title
	filter := func(b model.BadgeRecord) bool {
		return b.Title == "Explore Generative AI in Agent Platform"
	}

	res := Evaluate(data, student, 1, filter, nil)
	if res.TotalBadges != 10 {
		t.Errorf("expected TotalBadges 10, got %d", res.TotalBadges)
	}
	if res.CountedBadges != 1 {
		t.Fatalf("expected 1 counted badge, got %d", res.CountedBadges)
	}
	if res.Badges[0].Title != "Explore Generative AI in Agent Platform" {
		t.Errorf("unexpected badge title: %s", res.Badges[0].Title)
	}
	if res.Status != model.StatusComplete {
		t.Errorf("expected StatusComplete with goal 1, got %v", res.Status)
	}
}
