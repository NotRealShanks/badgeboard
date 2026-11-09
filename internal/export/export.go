package export

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
)

// Summary provides high-level aggregation metrics for exports and dashboards.
type Summary struct {
	GeneratedAt       time.Time `json:"generated_at"`
	Goal              int       `json:"goal"`
	TotalStudents     int       `json:"total_students"`
	CompleteCount     int       `json:"complete_count"`
	InProgressCount   int       `json:"in_progress_count"`
	NoBadgesCount     int       `json:"no_badges_count"`
	PrivateOrNotFound int       `json:"private_or_not_found_count"`
	ErrorCount        int       `json:"error_count"`
	Anonymized        bool      `json:"anonymized"`
}

// ExportPayload wraps the structured JSON output consumed by the dashboard and CLI export.
type ExportPayload struct {
	Summary Summary               `json:"summary"`
	Results []model.ProfileResult `json:"results"`
}

// ComputeSummary calculates aggregated campaign metrics.
func ComputeSummary(results []model.ProfileResult, goal int, anonymized bool) Summary {
	if goal <= 0 {
		goal = 10
	}

	summary := Summary{
		GeneratedAt:   time.Now().UTC(),
		Goal:          goal,
		TotalStudents: len(results),
		Anonymized:    anonymized,
	}

	for _, r := range results {
		switch r.Status {
		case model.StatusComplete:
			summary.CompleteCount++
		case model.StatusInProgress:
			summary.InProgressCount++
		case model.StatusNoBadges:
			summary.NoBadgesCount++
		case model.StatusPrivateOrNotFound:
			summary.PrivateOrNotFound++
		case model.StatusInvalidURL, model.StatusNetworkError, model.StatusParseError, model.StatusRateLimited:
			summary.ErrorCount++
		}
	}

	return summary
}

// AnonymizeResult produces a privacy-preserving copy of a ProfileResult.
// In anonymized mode:
// - Roster name is replaced with initials + short 4-char UUID suffix
// - Display name is replaced with the same anonymized pseudonym
// - Email is scrubbed
// - Profile URL and full UUID are scrubbed
// - Per-badge URLs are dropped
func AnonymizeResult(r model.ProfileResult) model.ProfileResult {
	copyRes := r

	sourceName := r.Student.Name
	if sourceName == "" {
		sourceName = r.DisplayName
	}

	anonName := AnonymizeName(sourceName, r.Student.UUID)
	copyRes.Student.Name = anonName
	copyRes.Student.ProfileURL = ""
	copyRes.Student.UUID = ""
	copyRes.Student.Email = ""
	copyRes.Student.Team = r.Student.Team

	if r.DisplayName != "" {
		copyRes.DisplayName = anonName
	}

	// Drop per-badge URLs in anonymized output
	if len(r.Badges) > 0 {
		copyRes.Badges = make([]model.BadgeRecord, len(r.Badges))
		for i, b := range r.Badges {
			copyRes.Badges[i] = b
			copyRes.Badges[i].URL = ""
		}
	}

	return copyRes
}

// AnonymizeName produces deterministic initials or short pseudonyms with a 4-char suffix.
// E.g. "Alice Johnson" with UUID "...ddd7" -> "A. J. (ddd7)".
func AnonymizeName(name, uuid string) string {
	uuidSuffix := ""
	cleanUUID := strings.TrimSpace(uuid)
	if len(cleanUUID) >= 4 {
		uuidSuffix = cleanUUID[len(cleanUUID)-4:]
	} else if cleanUUID != "" {
		uuidSuffix = cleanUUID
	} else {
		hash := sha256.Sum256([]byte(name))
		uuidSuffix = hex.EncodeToString(hash[:])[:4]
	}

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return fmt.Sprintf("Participant (%s)", uuidSuffix)
	}

	parts := strings.Fields(trimmedName)
	if len(parts) == 1 {
		return fmt.Sprintf("%c. (%s)", parts[0][0], uuidSuffix)
	}

	initials := make([]string, len(parts))
	for i, p := range parts {
		if len(p) > 0 {
			initials[i] = string(p[0]) + "."
		}
	}
	return fmt.Sprintf("%s (%s)", strings.Join(initials, " "), uuidSuffix)
}

// sanitizeCSVField neutralizes formula injection in spreadsheets.
// Cells beginning with '=', '+', '-', '@', '\t', or '\r' are prefixed with a single quote.
func sanitizeCSVField(s string) string {
	if s == "" {
		return ""
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	default:
		return s
	}
}

// JSON exports results and summary as formatted JSON to w.
func JSON(w io.Writer, results []model.ProfileResult, goal int, anonymize bool) error {
	exportResults := results
	if anonymize {
		exportResults = make([]model.ProfileResult, len(results))
		for i, r := range results {
			exportResults[i] = AnonymizeResult(r)
		}
	}

	payload := ExportPayload{
		Summary: ComputeSummary(exportResults, goal, anonymize),
		Results: exportResults,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// CSV exports results in tabular CSV format to w.
func CSV(w io.Writer, results []model.ProfileResult, goal int, anonymize bool) error {
	writer := csv.NewWriter(w)
	defer writer.Flush()

	header := []string{
		"Name",
		"Profile URL",
		"UUID",
		"Status",
		"Counted Badges",
		"Total Badges",
		"Goal",
		"Last Badge Date",
		"League",
		"Points",
		"Error",
	}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("writing CSV header: %w", err)
	}

	if goal <= 0 {
		goal = 10
	}

	for _, r := range results {
		res := r
		if anonymize {
			res = AnonymizeResult(r)
		}

		lastDate := ""
		if res.LastBadgeEarnedAt != nil && !res.LastBadgeEarnedAt.IsZero() {
			lastDate = res.LastBadgeEarnedAt.Format("2006-01-02")
		}

		record := []string{
			sanitizeCSVField(res.Student.Name),
			sanitizeCSVField(res.Student.ProfileURL),
			sanitizeCSVField(res.Student.UUID),
			sanitizeCSVField(res.Status.String()),
			sanitizeCSVField(strconv.Itoa(res.CountedBadges)),
			sanitizeCSVField(strconv.Itoa(res.TotalBadges)),
			sanitizeCSVField(strconv.Itoa(goal)),
			sanitizeCSVField(lastDate),
			sanitizeCSVField(res.League),
			sanitizeCSVField(strconv.Itoa(res.Points)),
			sanitizeCSVField(res.Error),
		}

		if err := writer.Write(record); err != nil {
			return fmt.Errorf("writing CSV row: %w", err)
		}
	}

	return nil
}
