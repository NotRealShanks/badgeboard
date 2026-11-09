package parse

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/config"
	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/PuerkitoBio/goquery"
)

var (
	badgeURLPattern  = regexp.MustCompile(`(?i)(?:https?://[^/]+)?/public_profiles/[^/]+/badges/(\d+)`)
	earnedPattern    = regexp.MustCompile(`(?i)Earned\s+([A-Za-z]{3}\s+\d{1,2},\s+\d{4}(?:\s+[A-Za-z0-9]+)?)`)
	pointsPattern    = regexp.MustCompile(`(?i)(\d+)\s+points?`)
	privateKeywords  = []string{"sign in", "not found", "404", "private", "bad gateway", "internal server error", "unauthorized", "forbidden"}
	dateParseFormats = []string{
		"Jan _2, 2006 MST",
		"Jan 02, 2006 MST",
		"Jan 2, 2006 MST",
		"Jan _2, 2006",
		"Jan 02, 2006",
		"Jan 2, 2006",
		time.RFC3339,
		"2006-01-02",
	}
)

// ProfileData holds the raw parsed components extracted from a public profile HTML page.
type ProfileData struct {
	DisplayName string
	League      string
	Points      int
	Badges      []model.BadgeRecord
	IsPrivate   bool
}

// Parse extracts profile data from raw HTML bytes without performing any I/O.
// It detects whether the profile is private/inaccessible or a valid profile page.
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

	// Check title for error or sign-in signals
	for _, kw := range privateKeywords {
		if strings.Contains(lowerTitle, kw) {
			return &ProfileData{IsPrivate: true}, nil
		}
	}

	// Look for the main profile heading (h1)
	h1 := doc.Find("h1").First()
	h1Text := strings.TrimSpace(h1.Text())
	lowerH1 := strings.ToLower(h1Text)

	if h1Text == "" {
		// Missing profile heading indicates private, redirected, or 404 page
		return &ProfileData{IsPrivate: true}, nil
	}

	for _, kw := range privateKeywords {
		if strings.Contains(lowerH1, kw) {
			return &ProfileData{IsPrivate: true}, nil
		}
	}

	// Check for explicit private profile indicators in body text
	bodyText := strings.ToLower(doc.Find("body").Text())
	if strings.Contains(bodyText, "this profile is private") ||
		strings.Contains(bodyText, "sign in to view this profile") {
		return &ProfileData{IsPrivate: true}, nil
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

		// Image URL if present in child img
		if img := s.Find("img").First(); img.Length() > 0 {
			if src, exists := img.Attr("src"); exists {
				badge.ImageURL = strings.TrimSpace(src)
			}
		}

		// Traverse badge container or parent to find title and earned date
		parent := s.Parent()
		if parent.Length() > 0 {
			// Find title: look for span with title class or sibling spans
			parent.Find("span").Each(func(_ int, span *goquery.Selection) {
				text := strings.TrimSpace(span.Text())
				if text == "" {
					return
				}
				if earnedMatch := earnedPattern.FindStringSubmatch(text); len(earnedMatch) > 1 {
					badge.RawEarned = text
					badge.EarnedAt = parseDate(earnedMatch[1])
				} else if badge.Title == "" && !strings.HasPrefix(strings.ToLower(text), "earned") {
					badge.Title = text
				}
			})

			// Fallback title from ql-button or aria-label if not found
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

// Evaluate evaluates a parsed ProfileData against student metadata and campaign config.
func Evaluate(data *ProfileData, student model.Student, cfg *config.Config, parseErr error) model.ProfileResult {
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

	// Use display name if student name is empty
	if result.Student.Name == "" && data.DisplayName != "" {
		result.Student.Name = data.DisplayName
	}

	// Filter eligible badges according to optional config window & required_badges
	eligibleBadges := data.Badges
	if cfg != nil {
		eligibleBadges = cfg.FilterEligibleBadges(data.Badges)
	}

	result.Badges = eligibleBadges
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

	// Evaluate final status
	if len(data.Badges) == 0 {
		result.Status = model.StatusNoBadges
	} else if cfg != nil {
		result.Status = cfg.EvaluateStatus(len(eligibleBadges))
	} else {
		if len(eligibleBadges) >= config.DefaultGoal {
			result.Status = model.StatusComplete
		} else if len(eligibleBadges) > 0 {
			result.Status = model.StatusInProgress
		} else {
			result.Status = model.StatusNoBadges
		}
	}

	return result
}

// ParseResult is a convenience function that parses HTML bytes and evaluates the result.
func ParseResult(html []byte, student model.Student, cfg *config.Config) model.ProfileResult {
	data, err := Parse(html)
	return Evaluate(data, student, cfg, err)
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

func parseDate(dateStr string) time.Time {
	clean := strings.TrimSpace(dateStr)
	// Replace internal multiple spaces (e.g. "Nov  3" -> "Nov 3") when trying formats with single space
	for _, format := range dateParseFormats {
		if t, err := time.Parse(format, clean); err == nil {
			return t
		}
	}
	return time.Time{}
}
