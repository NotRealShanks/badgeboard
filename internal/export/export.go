package export

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/NotRealShanks/badgeboard/internal/roster"
)

var (
	uuidRegex  = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	urlRegex   = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
	emailRegex = regexp.MustCompile(`(?i)[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)

	// DemoSalt is the fixed HMAC key used for reproducible demo and testing data.
	DemoSalt = []byte("badgeboard-demo-salt-fixed-reproducible-key-2026")
)

// ResolveSalt retrieves the HMAC salt from saltFile, demoSalt flag, or BADGEBOARD_SALT environment variable.
// If anonymization is active and no salt is available, it returns an error refusing execution.
func ResolveSalt(saltFile string, demoSalt bool) ([]byte, error) {
	if demoSalt {
		return DemoSalt, nil
	}
	if saltFile != "" {
		data, err := os.ReadFile(saltFile)
		if err != nil {
			return nil, fmt.Errorf("reading salt file %q: %w", saltFile, err)
		}
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) == 0 {
			return nil, fmt.Errorf("salt file %q is empty", saltFile)
		}
		return trimmed, nil
	}
	if envSalt := strings.TrimSpace(os.Getenv("BADGEBOARD_SALT")); envSalt != "" {
		return []byte(envSalt), nil
	}
	return nil, errors.New("anonymization requires a secret salt: set BADGEBOARD_SALT environment variable, pass --salt-file, or pass --demo-salt for demo/testing")
}

// DerivePseudonym derives an irreversible pseudonym in the format "Learner-XXXX"
// from an identifier (such as a UUID) using HMAC-SHA256 keyed with a secret salt.
func DerivePseudonym(id string, salt []byte) string {
	if len(salt) == 0 {
		salt = DemoSalt
	}
	clean := strings.ToLower(strings.TrimSpace(id))
	mac := hmac.New(sha256.New, salt)
	mac.Write([]byte(clean))
	sum := mac.Sum(nil)
	return fmt.Sprintf("Learner-%04X", sum[:2])
}

// ScrubText removes URLs, UUIDs, emails, and names from text (such as error messages).
func ScrubText(text string, names ...string) string {
	if text == "" {
		return ""
	}
	s := urlRegex.ReplaceAllString(text, "[redacted url]")
	s = uuidRegex.ReplaceAllString(s, "[redacted uuid]")
	s = emailRegex.ReplaceAllString(s, "[redacted email]")
	for _, n := range names {
		if trimmed := strings.TrimSpace(n); trimmed != "" {
			s = strings.ReplaceAll(s, trimmed, "[redacted name]")
		}
	}
	return s
}

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
	BadRowCount       int       `json:"bad_row_count,omitempty"`
	Anonymized        bool      `json:"anonymized"`
}

// ExportPayload wraps the structured JSON output consumed by the dashboard and CLI export.
type ExportPayload struct {
	Summary Summary               `json:"summary"`
	Results []model.ProfileResult `json:"results"`
	BadRows []roster.BadRowReport `json:"bad_rows,omitempty"`
}

// ComputeSummary calculates aggregated campaign metrics.
func ComputeSummary(results []model.ProfileResult, goal int, anonymized bool) Summary {
	if goal <= 0 {
		goal = 10
	}

	genAt := time.Now().UTC().Truncate(time.Hour)

	summary := Summary{
		GeneratedAt:   genAt,
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
// - Roster name is replaced with "Learner-XXXX" derived via HMAC-SHA256 of UUID with a secret salt
// - Display name is replaced with the same anonymized pseudonym
// - Email is scrubbed
// - Profile URL and full UUID are scrubbed
// - Error messages are scrubbed of URLs, UUIDs, emails, and names
// - In Public mode (default without includeBadges): badge list, per-badge dates, badge titles,
//   badge URLs, league, points, and full-precision timestamps are omitted.
// - In Full Anonymized mode (with includeBadges): badge titles and dates are retained, but URLs are dropped.
func AnonymizeResult(r model.ProfileResult, salt []byte, includeBadges ...bool) model.ProfileResult {
	withBadges := len(includeBadges) > 0 && includeBadges[0]
	copyRes := r

	targetID := r.Student.UUID
	if targetID == "" {
		targetID = r.Student.ProfileURL
	}
	if targetID == "" {
		targetID = r.Student.Name
	}
	if targetID == "" {
		targetID = r.DisplayName
	}

	anonName := DerivePseudonym(targetID, salt)

	copyRes.Student.Name = anonName
	copyRes.Student.ProfileURL = ""
	copyRes.Student.UUID = ""
	copyRes.Student.Email = ""
	copyRes.Student.Team = r.Student.Team

	if r.DisplayName != "" {
		copyRes.DisplayName = anonName
	}

	// Always omit league and points in anonymized public mode
	copyRes.League = ""
	copyRes.Points = 0

	// Format last badge date string (date only)
	if copyRes.LastBadgeDate == "" && r.LastBadgeEarnedAt != nil && !r.LastBadgeEarnedAt.IsZero() {
		copyRes.LastBadgeDate = r.LastBadgeEarnedAt.Format("2006-01-02")
	}

	// Clear full-precision timestamp and runtime cache status
	copyRes.FetchedAt = nil
	copyRes.FromCache = false

	if withBadges {
		// Include badge details (with scrubbed URL and date-only midnight timestamps)
		if len(r.Badges) > 0 {
			copyRes.Badges = make([]model.BadgeRecord, len(r.Badges))
			for i, b := range r.Badges {
				copyRes.Badges[i] = b
				copyRes.Badges[i].URL = ""
				copyRes.Badges[i].ImageURL = ""
			}
		}
	} else {
		// Public tier: omit badge details, per-badge dates, badge titles, and full-precision timestamps
		copyRes.Badges = nil
		copyRes.LastBadgeEarnedAt = nil
	}

	// Scrub any error strings containing URLs, UUIDs, emails, or names
	copyRes.Error = ScrubText(r.Error, r.Student.Name, r.DisplayName)

	return copyRes
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
func JSON(w io.Writer, results []model.ProfileResult, goal int, anonymize bool, salt []byte, includeBadges ...bool) error {
	withBadges := len(includeBadges) > 0 && includeBadges[0]
	return JSONWithBadRows(w, results, nil, goal, anonymize, salt, withBadges)
}

// JSONWithBadRows exports results, summary, and bad-rows report as formatted JSON to w.
// In anonymized mode, the bad-rows report is dropped to prevent data leakage of raw input URLs/names.
func JSONWithBadRows(w io.Writer, results []model.ProfileResult, badRows []roster.BadRowReport, goal int, anonymize bool, salt []byte, includeBadges ...bool) error {
	withBadges := len(includeBadges) > 0 && includeBadges[0]
	exportResults := results
	if anonymize {
		exportResults = make([]model.ProfileResult, len(results))
		for i, r := range results {
			exportResults[i] = AnonymizeResult(r, salt, withBadges)
		}
	}

	summary := ComputeSummary(exportResults, goal, anonymize)
	summary.BadRowCount = len(badRows)

	var exportBadRows []roster.BadRowReport
	if !anonymize && len(badRows) > 0 {
		exportBadRows = badRows
	}

	payload := ExportPayload{
		Summary: summary,
		Results: exportResults,
		BadRows: exportBadRows,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// CSV exports results in tabular CSV format to w.
func CSV(w io.Writer, results []model.ProfileResult, goal int, anonymize bool, salt []byte) error {
	writer := csv.NewWriter(w)
	defer writer.Flush()

	var header []string
	if anonymize {
		header = []string{
			"Name",
			"Status",
			"Counted Badges",
			"Total Badges",
			"Goal",
			"Last Badge Date",
			"Error",
		}
	} else {
		header = []string{
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
			res = AnonymizeResult(r, salt, false)
		}

		lastDate := res.LastBadgeDate
		if lastDate == "" && res.LastBadgeEarnedAt != nil && !res.LastBadgeEarnedAt.IsZero() {
			lastDate = res.LastBadgeEarnedAt.Format("2006-01-02")
		}

		var record []string
		if anonymize {
			record = []string{
				sanitizeCSVField(res.Student.Name),
				sanitizeCSVField(res.Status.String()),
				sanitizeCSVField(strconv.Itoa(res.CountedBadges)),
				sanitizeCSVField(strconv.Itoa(res.TotalBadges)),
				sanitizeCSVField(strconv.Itoa(goal)),
				sanitizeCSVField(lastDate),
				sanitizeCSVField(res.Error),
			}
		} else {
			record = []string{
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
		}

		if err := writer.Write(record); err != nil {
			return fmt.Errorf("writing CSV row: %w", err)
		}
	}

	return nil
}
