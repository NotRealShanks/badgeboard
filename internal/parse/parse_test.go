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
	if result.CountedBadges != 10 {
		t.Fatalf("GATE FAILURE: expected CountedBadges 10, got %d", result.CountedBadges)
	}

	if len(result.Badges) != 10 {
		t.Fatalf("expected 10 badges in Badges slice, got %d", len(result.Badges))
	}

	if result.DisplayName != "Sample Learner" {
		t.Errorf("expected display name %q, got %q", "Sample Learner", result.DisplayName)
	}

	if result.Student.Name != "Sample Learner" {
		t.Errorf("expected Student.Name %q, got %q", "Sample Learner", result.Student.Name)
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
				t.Errorf("expected display name %q, got %q", tc.expectDisplay, res.DisplayName)
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

	// Evaluate the EXACT SAME parsed data with goal 4 -> Complete!
	resGoal4 := Evaluate(data, student, 4, nil, nil)
	if resGoal4.Status != model.StatusComplete {
		t.Errorf("expected StatusComplete with goal 4, got %v", resGoal4.Status)
	}
	if resGoal4.TotalBadges != 4 || resGoal4.CountedBadges != 4 {
		t.Errorf("badges count mismatch: total=%d, counted=%d", resGoal4.TotalBadges, resGoal4.CountedBadges)
	}
}

func TestRequiredBadges_TotalGreaterThanZeroAndCountedZero_YieldsInProgress(t *testing.T) {
	html := loadFixture(t, "sample_profile.html") // 10 badges on profile
	data, err := Parse(html)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Filter with a required badge title that does NOT match any earned badge
	filterNoneMatched := func(b model.BadgeRecord) bool {
		return b.Title == "Non-Existent Unearned Badge"
	}

	student := model.Student{UUID: "be556f7b-ab4b-4be7-82fb-9be45202ddd7"}
	res := Evaluate(data, student, 10, filterNoneMatched, nil)

	if res.TotalBadges != 10 {
		t.Errorf("expected TotalBadges 10, got %d", res.TotalBadges)
	}
	if res.CountedBadges != 0 {
		t.Errorf("expected CountedBadges 0, got %d", res.CountedBadges)
	}
	// Crucial rule: must NEVER be reported as NoBadges!
	if res.Status != model.StatusInProgress {
		t.Errorf("CRITICAL: expected StatusInProgress when TotalBadges > 0 and CountedBadges == 0, got %v", res.Status)
	}
}

func TestPaginationDetection(t *testing.T) {
	// Subtest 1: Real sample does NOT trigger ErrPaginationDetected
	t.Run("real sample has no pagination error", func(t *testing.T) {
		html := loadFixture(t, "sample_profile.html")
		_, err := Parse(html)
		if err != nil {
			t.Fatalf("expected real sample to parse cleanly without pagination error, got: %v", err)
		}
	})

	// Subtest 2: Pagination markup in nav or footer outside badge container is ignored
	t.Run("nav/footer pagination outside badge container is ignored", func(t *testing.T) {
		htmlWithNavPagination := `
<!DOCTYPE html>
<html>
<head><title>Test User | Google Skills</title></head>
<body>
  <nav class="pagination"><a href="/topics?page=2">Next Topic</a></nav>
  <h1>Test User</h1>
  <p>Member since 2026</p>
  <div class="profile-badges">
    <div class="profile-badge">
      <a href="/public_profiles/1111/badges/101">Badge 1</a>
      <span>Earned Nov  1, 2026 EST</span>
    </div>
  </div>
  <footer><div class="pagination"><a href="/footer?page=2">Footer Page</a></div></footer>
</body>
</html>`
		data, err := Parse([]byte(htmlWithNavPagination))
		if err != nil {
			t.Fatalf("expected pagination outside badge container to be ignored, got: %v", err)
		}
		if len(data.Badges) != 1 {
			t.Errorf("expected 1 badge parsed, got %d", len(data.Badges))
		}
	})

	// Subtest 3: Load more / pagination INSIDE badge container triggers ErrPaginationDetected
	t.Run("load more inside badge container triggers ErrPaginationDetected", func(t *testing.T) {
		htmlWithBadgePagination := `
<!DOCTYPE html>
<html>
<head><title>Test User | Google Skills</title></head>
<body>
  <h1>Test User</h1>
  <p>Member since 2026</p>
  <div class="profile-badges">
    <div class="profile-badge">
      <a href="/public_profiles/1111/badges/101">Badge 1</a>
      <span>Earned Nov  1, 2026 EST</span>
    </div>
    <div class="load-more">
      <button>Load More Badges</button>
    </div>
  </div>
</body>
</html>`
		_, err := Parse([]byte(htmlWithBadgePagination))
		if err == nil {
			t.Fatalf("expected error when 'load more' is inside badge container, got nil")
		}
		if !errors.Is(err, ErrPaginationDetected) {
			t.Errorf("expected ErrPaginationDetected, got %v", err)
		}
	})
}

func TestDateParsing_IndependentOfMachineTZ(t *testing.T) {
	origLocal := time.Local
	defer func() { time.Local = origLocal }()

	testZones := []struct {
		name string
		loc  *time.Location
	}{
		{"UTC", time.UTC},
		{"Asia/Kolkata", time.FixedZone("IST", 5*3600+1800)},
		{"America/New_York", NYLocation()},
		{"Europe/London", time.FixedZone("BST", 1*3600)},
		{"Pacific/Honolulu", time.FixedZone("HST", -10*3600)},
	}

	dateStr := "Earned Nov  3, 2026 EST"
	nyLoc := NYLocation()

	for _, tc := range testZones {
		t.Run("time.Local="+tc.name, func(t *testing.T) {
			time.Local = tc.loc

			parsed := ParseDate(dateStr)
			if parsed.IsZero() {
				t.Fatalf("failed to parse date %q with time.Local=%s", dateStr, tc.name)
			}

			inNY := parsed.In(nyLoc)
			year, month, day := inNY.Date()

			if year != 2026 || month != time.November || day != 3 {
				t.Errorf("with time.Local=%s, expected 2026-11-03, got %04d-%02d-%02d", tc.name, year, month, day)
			}
		})
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
