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
	pointsPattern   = regexp.MustCompile(`(?i)(\d+)\s+points?`)

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

	// Markers indicating page pagination or unhandled load more controls
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

// ErrPaginationDetected is returned when pagination or "load more" controls are encountered.
var ErrPaginationDetected = errors.New("profile pagination or 'load more' control detected; pagination parsing is unverified so aborting to avoid truncated badge count")

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
// It distinguishes between:
// - Valid public profiles (with 0 or more badges)
// - Private or inaccessible profiles
// - Parse errors (malformed HTML, server error pages, missing markers, or unverified pagination)
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

	// 2. Check for unverified pagination or load more controls -> ParseError
	for _, sel := range paginationSelectors {
		if doc.Find(sel).Length() > 0 {
			return nil, ErrPaginationDetected
		}
	}
	for _, kw := range paginationKeywords {
		if strings.Contains(bodyText, kw) {
			return nil, ErrPaginationDetected
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
		// Missing profile heading without private indicators is malformed profile markup
		return nil, fmt.Errorf("missing profile h1 heading")
	}

	// Check if h1 text itself is a server error or private indicator
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

	// 5. Check for positive evidence of a real public profile:
	// h1 alone is not enough to confirm a public profile. There must be at least one
	// additional marker present in the real sample:
	// - "Member since" text
	// - League block (.profile-league or league headline)
	// - Points block
	// - Avatar (.profile-avatar or ql-avatar)
	// - Profile badges container (.profile-badges)
	hasMemberSince := strings.Contains(bodyText, "member since")
	hasLeague := doc.Find(".profile-league, h2.ql-headline-medium").Length() > 0
	hasPoints := pointsPattern.MatchString(bodyText)
	hasAvatar := doc.Find(".profile-avatar, ql-avatar").Length() > 0
	hasBadgesContainer := doc.Find(".profile-badges").Length() > 0

	hasPositiveProfileMarkers := hasMemberSince || hasLeague || hasPoints || hasAvatar || hasBadgesContainer

	if !hasPositiveProfileMarkers {
		// If zero badges and no positive profile markers, reject as ParseError (never report as NoBadges)
		return nil, fmt.Errorf("missing positive profile markers (member since, league/points, avatar) to confirm public profile")
	}

	data := &ProfileData{
		DisplayName: h1Text,
		IsPrivate:   false,
	}

	// Extract optional League
	if leagueEl := doc.Find(".profile-league h2").First(); leagueEl.Length() > 0 {
		data.League = strings.TrimSpace(leagueEl.Text())
	}

	// Extract optional Points
	if ptsEl := doc.Find(".profile-league strong").First(); ptsEl.Length() > 0 {
		match := pointsPattern.FindStringSubmatch(ptsEl.Text())
		if len(match) > 1 {
			if pts, err := strconv.Atoi(match[1]); err == nil {
				data.Points = pts
			}
		}
	}

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
		return result
	}

	if data == nil || data.IsPrivate {
		result.Status = model.StatusPrivateOrNotFound
		return result
	}

	result.DisplayName = data.DisplayName
	result.League = data.League
	result.Points = data.Points
	result.TotalBadges = len(data.Badges)

	if result.Student.Name == "" && data.DisplayName != "" {
		result.Student.Name = data.DisplayName
	}

	// Apply optional badge filter
	eligibleBadges := make([]model.BadgeRecord, 0, len(data.Badges))
	for _, b := range data.Badges {
		if badgeFilter == nil || badgeFilter(b) {
			eligibleBadges = append(eligibleBadges, b)
		}
	}

	result.Badges = eligibleBadges
	result.CountedBadges = len(eligibleBadges)
	result.BadgeCount = len(eligibleBadges)

	// Determine latest badge earned date
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

	// Evaluate status using model.EvaluateStatus
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
func ParseDate(dateStr string) time.Time {
	clean := strings.TrimSpace(dateStr)
	// Strip "Earned" prefix if present
	clean = regexp.MustCompile(`(?i)^earned\s+`).ReplaceAllString(clean, "")
	// Remove trailing time zone name if present (e.g. "EST", "EDT")
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
