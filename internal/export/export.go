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
	GeneratedAt        time.Time `json:"generated_at"`
	Goal               int       `json:"goal"`
	TotalStudents      int       `json:"total_students"`
	CompleteCount      int       `json:"complete_count"`
	InProgressCount    int       `json:"in_progress_count"`
	NoBadgesCount      int       `json:"no_badges_count"`
	PrivateOrNotFound  int       `json:"private_or_not_found_count"`
	ErrorCount         int       `json:"error_count"`
	Anonymized         bool      `json:"anonymized"`
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
// Replaces student names with initials and short hash, removes email, and sanitizes display name.
func AnonymizeResult(r model.ProfileResult) model.ProfileResult {
	copyRes := r

	anonName := AnonymizeName(r.Student.Name, r.Student.UUID)
	copyRes.Student.Name = anonName
	copyRes.Student.Email = "" // Scrub email entirely
	copyRes.Student.Team = r.Student.Team

	if r.DisplayName != "" {
		copyRes.DisplayName = anonName
	}

	return copyRes
}

// AnonymizeName produces deterministic initials or short pseudonyms.
// E.g. "Alice Johnson" -> "A. J. (ddd7)" or "Student-a1b2c3".
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
		return fmt.Sprintf("Participant %s", uuidSuffix)
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

	// Write header
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
			res.Student.Name,
			res.Student.ProfileURL,
			res.Student.UUID,
			res.Status.String(),
			strconv.Itoa(res.CountedBadges),
			strconv.Itoa(res.TotalBadges),
			strconv.Itoa(goal),
			lastDate,
			res.League,
			strconv.Itoa(res.Points),
			res.Error,
		}

		if err := writer.Write(record); err != nil {
			return fmt.Errorf("writing CSV row: %w", err)
		}
	}

	return nil
}
