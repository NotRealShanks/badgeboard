package parse

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/PuerkitoBio/goquery"
)

var (
	badgeURLPattern = regexp.MustCompile(`(?i)(?:https?://[^/]+)?/public_profiles/[^/]+/badges/(\d+)`)
	earnedPattern   = regexp.MustCompile(`(?i)Earned\s+([A-Za-z]{3}\s+\d{1,2},\s+\d{4})`)
	pointsPattern   = regexp.MustCompile(`(?i)\b(\d+)\s+points\b`)
	leaguePattern   = regexp.MustCompile(`(?i)\b[A-Za-z]+\s+League\b`)

	// Server error markers that indicate HTTP or infrastructure failure rather than profile content
	serverErrorKeywords = []string{
		"500", "502", "503", "504",
		"bad gateway", "internal server error", "service unavailable",
		"nginx", "cloudflare", "error 5",
	}

	// Explicit private profile markers
	privateKeywords = []string{
		"this profile is private",
		"private profile",
		"sign in to view this profile",
		"sign in to google skills",
	}

	// Markers indicating page pagination or unhandled load more controls inside badge container
	paginationSelectors = []string{
		".pagination",
		"ql-pagination",
		"[rel='next']",
		".load-more",
		"a.next_page",
		".next-page",
	}
	paginationKeywords = []string{
		"load more",
		"view more badges",
		"next page",
	}

	nyLocation = func() *time.Location {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			return time.FixedZone("EST", -5*3600)
		}
		return loc
	}()
)

// ErrPaginationDetected is returned when pagination or "load more" controls are encountered
// inside the badge list container.
var ErrPaginationDetected = errors.New("profile pagination or 'load more' control detected in badge container; pagination parsing is unverified so aborting to avoid truncated badge count")

// ProfileData holds the raw parsed components extracted from a public profile HTML page.
// The parser does not know about goals or campaign windows; it extracts raw profile facts.
type ProfileData struct {
	DisplayName string
	League      string
	Points      int
	Badges      []model.BadgeRecord
	IsPrivate   bool
}

// Parse extracts profile data from raw HTML bytes without performing any I/O.
func Parse(html []byte) (*ProfileData, error) {
	if len(bytes.TrimSpace(html)) == 0 {
		return nil, fmt.Errorf("empty html payload")
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parsing html: %w", err)
	}

	titleText := strings.TrimSpace(doc.Find("title").Text())
	lowerTitle := strings.ToLower(titleText)
	bodyText := strings.ToLower(doc.Find("body").Text())

	// 1. Detect server-level or gateway error pages -> ParseError
	for _, kw := range serverErrorKeywords {
		if strings.Contains(lowerTitle, kw) {
			return nil, fmt.Errorf("server error or gateway error markup detected (%s)", titleText)
		}
	}

	// 2. Locate badge list container for scoped pagination checks and badge discovery
	badgeContainer := doc.Find(".profile-badges").First()
	if badgeContainer.Length() == 0 {
		// Fallback: look for container holding elements with badge URLs
		doc.Find("a").Each(func(_ int, a *goquery.Selection) {
			if href, exists := a.Attr("href"); exists && badgeURLPattern.MatchString(href) {
				if badgeContainer.Length() == 0 {
					badgeContainer = a.Closest("div")
				}
			}
		})
	}

	// Scoped pagination check: ONLY inside the badge container, never triggered by nav or footer
	if badgeContainer.Length() > 0 {
		for _, sel := range paginationSelectors {
			if badgeContainer.Find(sel).Length() > 0 {
				return nil, ErrPaginationDetected
			}
		}
		containerText := strings.ToLower(badgeContainer.Text())
		for _, kw := range paginationKeywords {
			if strings.Contains(containerText, kw) {
				return nil, ErrPaginationDetected
			}
		}
	}

	// 3. Check for explicit private profile indicators
	for _, kw := range privateKeywords {
		if strings.Contains(bodyText, kw) || strings.Contains(lowerTitle, kw) {
			return &ProfileData{IsPrivate: true}, nil
		}
	}

	// 4. Look for the main profile heading (h1)
	h1 := doc.Find("h1").First()
	h1Text := strings.TrimSpace(h1.Text())
	lowerH1 := strings.ToLower(h1Text)

	if h1Text == "" {
		return nil, fmt.Errorf("missing profile h1 heading")
	}

	for _, kw := range serverErrorKeywords {
		if strings.Contains(lowerH1, kw) {
			return nil, fmt.Errorf("server error in h1: %s", h1Text)
		}
	}
	for _, kw := range privateKeywords {
		if strings.Contains(lowerH1, kw) {
			return &ProfileData{IsPrivate: true}, nil
		}
	}

	// 5. Positive evidence of a real public profile:
	// The h1 alone is not enough to confirm a public profile.
	// Positive secondary markers present in the real sample:
	// - Visible text: "member since" (e.g. "Member since 2026")
	// - Visible text matching points pattern: e.g. "339 points"
	// - Visible text in h2 matching league: e.g. "Silver League"
	// - Structural presence of the badge list container
	hasMemberSince := strings.Contains(bodyText, "member since")
	hasPoints := pointsPattern.MatchString(bodyText)

	hasLeague := false
	doc.Find("h2").Each(func(_ int, h2 *goquery.Selection) {
		if leaguePattern.MatchString(h2.Text()) {
			hasLeague = true
		}
	})
	hasBadgesContainer := doc.Find(".profile-badges").Length() > 0

	hasPositiveProfileMarkers := hasMemberSince || hasPoints || hasLeague || hasBadgesContainer

	if !hasPositiveProfileMarkers {
		// When an account is private, upstream redirects to the root landing page ("Google Skills")
		if lowerTitle == "google skills" || strings.HasPrefix(lowerTitle, "google cloud skills boost") || lowerTitle == "home" {
			return &ProfileData{IsPrivate: true}, nil
		}
		// Zero badges and missing profile markers on broken/unknown markup must resolve to ParseError, never NoBadges
		return nil, fmt.Errorf("missing positive profile markers (member since, points, league) to confirm public profile")
	}

	data := &ProfileData{
		DisplayName: h1Text,
		IsPrivate:   false,
	}

	// Extract optional League from h2
	doc.Find("h2").Each(func(_ int, h2 *goquery.Selection) {
		text := strings.TrimSpace(h2.Text())
		if leaguePattern.MatchString(text) && data.League == "" {
			data.League = text
		}
	})

	// Extract optional Points from strong or text
	doc.Find("strong, p, span").Each(func(_ int, el *goquery.Selection) {
		text := strings.TrimSpace(el.Text())
		match := pointsPattern.FindStringSubmatch(text)
		if len(match) > 1 && data.Points == 0 {
			if pts, err := strconv.Atoi(match[1]); err == nil {
				data.Points = pts
			}
		}
	})

	// Structural extraction of badge cards based on badge URL link pattern
	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		match := badgeURLPattern.FindStringSubmatch(href)
		if len(match) < 2 {
			return
		}
		badgeID := match[1]

		badge := model.BadgeRecord{
			ID:  badgeID,
			URL: normalizeBadgeURL(href),
		}

		if img := s.Find("img").First(); img.Length() > 0 {
			if src, exists := img.Attr("src"); exists {
				badge.ImageURL = strings.TrimSpace(src)
			}
		}

		parent := s.Parent()
		if parent.Length() > 0 {
			parent.Find("span").Each(func(_ int, span *goquery.Selection) {
				text := strings.TrimSpace(span.Text())
				if text == "" {
					return
				}
				if earnedMatch := earnedPattern.FindStringSubmatch(text); len(earnedMatch) > 1 {
					badge.RawEarned = text
					badge.EarnedAt = ParseDate(earnedMatch[1])
				} else if badge.Title == "" && !strings.HasPrefix(strings.ToLower(text), "earned") {
					badge.Title = text
				}
			})

			// Fallback title from ql-button aria attribute if not found
			if badge.Title == "" {
				if btn := parent.Find("ql-button").First(); btn.Length() > 0 {
					aria, exists := btn.Attr("arialabel")
					if !exists {
						aria, exists = btn.Attr("aria-label")
					}
					if !exists {
						aria, exists = btn.Attr("ariaLabel")
					}
					if exists {
						const prefix = "Learn more about "
						if strings.HasPrefix(aria, prefix) {
							badge.Title = strings.TrimSpace(strings.TrimPrefix(aria, prefix))
						} else {
							badge.Title = strings.TrimSpace(aria)
						}
					}
				}
			}
		}

		data.Badges = append(data.Badges, badge)
	})

	return data, nil
}

// Evaluate composes a ProfileResult from parsed ProfileData, student info, campaign goal,
// and an optional badge filter function (e.g. for required badges or date window).
// The parser itself remains completely agnostic of the goal.
func Evaluate(data *ProfileData, student model.Student, goal int, badgeFilter func(model.BadgeRecord) bool, parseErr error) model.ProfileResult {
	now := time.Now().UTC()
	result := model.ProfileResult{
		Student:   student,
		FetchedAt: now,
	}

	if parseErr != nil {
		result.Status = model.StatusParseError
		result.Error = parseErr.Error()
		if result.DisplayName == "" {
			result.DisplayName = model.FallbackProfileLabel(student.UUID)
		}
		if result.Student.Name == "" {
			result.Student.Name = result.DisplayName
		}
		return result
	}

	if data == nil || data.IsPrivate {
		result.Status = model.StatusPrivateOrNotFound
		if result.DisplayName == "" {
			result.DisplayName = model.FallbackProfileLabel(student.UUID)
		}
		if result.Student.Name == "" {
			result.Student.Name = result.DisplayName
		}
		return result
	}

	result.DisplayName = data.DisplayName
	result.League = data.League
	result.Points = data.Points
	result.TotalBadges = len(data.Badges)

	if result.Student.Name == "" && data.DisplayName != "" {
		result.Student.Name = data.DisplayName
	}
	if result.DisplayName == "" {
		result.DisplayName = model.FallbackProfileLabel(student.UUID)
	}
	if result.Student.Name == "" {
		result.Student.Name = result.DisplayName
	}

	eligibleBadges := make([]model.BadgeRecord, 0, len(data.Badges))
	for _, b := range data.Badges {
		if badgeFilter == nil || badgeFilter(b) {
			eligibleBadges = append(eligibleBadges, b)
		}
	}

	result.Badges = eligibleBadges
	result.CountedBadges = len(eligibleBadges)
	result.BadgeCount = len(eligibleBadges)

	var latest *time.Time
	for _, b := range eligibleBadges {
		if !b.EarnedAt.IsZero() {
			if latest == nil || b.EarnedAt.After(*latest) {
				tCopy := b.EarnedAt
				latest = &tCopy
			}
		}
	}
	result.LastBadgeEarnedAt = latest

	result.Status = model.EvaluateStatus(result.CountedBadges, goal, result.TotalBadges, data.IsPrivate)
	return result
}

func normalizeBadgeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return "https://www.skills.google" + raw
	}
	u, err := url.Parse(raw)
	if err == nil && u.IsAbs() {
		return raw
	}
	return "https://www.skills.google/" + raw
}

// ParseDate parses the earned date as a calendar date in America/New_York timezone.
// Handles strings like "Earned Nov  3, 2026 EST", "Nov  3, 2026", and single/double spaced days.
// It never references time.Local.
func ParseDate(dateStr string) time.Time {
	clean := strings.TrimSpace(dateStr)
	clean = regexp.MustCompile(`(?i)^earned\s+`).ReplaceAllString(clean, "")
	clean = regexp.MustCompile(`\s+[A-Za-z]{3,4}$`).ReplaceAllString(clean, "")

	formats := []string{
		"Jan _2, 2006",
		"Jan 02, 2006",
		"Jan 2, 2006",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.ParseInLocation(format, clean, nyLocation); err == nil {
			return t
		}
	}
	return time.Time{}
}

// NYLocation returns the America/New_York timezone location used for calendar date parsing.
func NYLocation() *time.Location {
	return nyLocation
}
