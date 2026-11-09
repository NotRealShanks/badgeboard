package roster

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/NotRealShanks/badgeboard/internal/model"
)

const TargetHeaderURL = "Google Skills Profile URL"

var (
	uuidRegex = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	allowedProfileHosts = map[string]bool{
		"www.skills.google":       true,
		"skills.google":           true,
		"cloudskillsboost.google": true,
	}
)

var (
	ErrEmptyRoster  = errors.New("roster is empty or contains no valid rows")
	ErrHeaderNotFound = errors.New("profile URL column header not found and auto-detection failed")
)

// BadRowReport records a malformed or invalid row encountered in the CSV.
type BadRowReport struct {
	RowNumber int    `json:"row_number"`
	RawValue  string `json:"raw_value"`
	Reason    string `json:"reason"`
}

// DuplicateReport records a duplicate participant detected during loading.
type DuplicateReport struct {
	RowNumber      int    `json:"row_number"`
	FirstRowNumber int    `json:"first_row_number"`
	UUID           string `json:"uuid"`
	Name           string `json:"name,omitempty"`
}

// Roster holds the valid students and parsing reports loaded from a CSV.
type Roster struct {
	Students   []model.Student   `json:"students"`
	BadRows    []BadRowReport    `json:"bad_rows,omitempty"`
	Duplicates []DuplicateReport `json:"duplicates,omitempty"`
}

// NormalizeURL strips whitespace, trailing slashes, query strings, and fragments.
// Accepts only the /public_profiles/<uuid> shape on skills.google or cloudskillsboost.google hosts.
// Returns canonical URL (https://www.skills.google/public_profiles/<uuid>) and UUID.
func NormalizeURL(raw string) (string, string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", "", fmt.Errorf("empty URL")
	}

	// Remove leading UTF-8 BOM if present
	clean = strings.TrimPrefix(clean, "\ufeff")

	// Prepend https:// if scheme is missing
	if !strings.HasPrefix(clean, "http://") && !strings.HasPrefix(clean, "https://") {
		clean = "https://" + clean
	}

	u, err := url.Parse(clean)
	if err != nil {
		return "", "", fmt.Errorf("malformed URL: %w", err)
	}

	host := strings.ToLower(u.Hostname())
	if !allowedProfileHosts[host] {
		return "", "", fmt.Errorf("unsupported host %q (expected skills.google or cloudskillsboost.google)", host)
	}

	// Strip trailing slashes, query parameters, and fragments
	path := strings.TrimRight(u.Path, "/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) != 2 || parts[0] != "public_profiles" {
		return "", "", fmt.Errorf("invalid path shape %q (expected /public_profiles/<uuid>)", u.Path)
	}

	candidateUUID := parts[1]
	if !uuidRegex.MatchString(candidateUUID) {
		return "", "", fmt.Errorf("invalid profile UUID format %q", candidateUUID)
	}

	canonicalURL := fmt.Sprintf("https://www.skills.google/public_profiles/%s", strings.ToLower(candidateUUID))
	return canonicalURL, strings.ToLower(candidateUUID), nil
}

// LoadFile reads and parses a CSV roster from disk.
func LoadFile(path string) (*Roster, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening roster file %q: %w", path, err)
	}
	defer f.Close()

	return Load(f)
}

// Load reads and parses a CSV roster from an io.Reader.
// Handles UTF-8 BOM, blank lines, missing columns, auto-detection, bad rows, and deduplication.
// One bad row must never abort the run.
func Load(r io.Reader) (*Roster, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading roster data: %w", err)
	}

	// Strip UTF-8 byte order mark (BOM)
	for bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	}

	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1 // Allow variable fields per record
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing CSV: %w", err)
	}

	if len(records) == 0 {
		return nil, ErrEmptyRoster
	}

	// 1. Identify columns from header
	headers := records[0]
	urlCol := -1
	nameCol := -1
	emailCol := -1
	teamCol := -1

	for idx, h := range headers {
		trimmed := strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))
		lower := strings.ToLower(trimmed)

		// Primary target: exact case-insensitive match for "Google Skills Profile URL"
		if strings.EqualFold(trimmed, TargetHeaderURL) {
			urlCol = idx
			continue
		}

		if lower == "name" || lower == "student name" || lower == "full name" {
			nameCol = idx
		} else if lower == "email" || lower == "student email" || lower == "email address" {
			emailCol = idx
		} else if lower == "team" || lower == "team name" || lower == "group" {
			teamCol = idx
		}
	}

	// Fallback: auto-detect column containing skills.google URLs if exact header not found
	if urlCol == -1 {
		urlCol = detectURLColumn(records)
	}

	if urlCol == -1 {
		return nil, ErrHeaderNotFound
	}

	roster := &Roster{
		Students:   make([]model.Student, 0),
		BadRows:    make([]BadRowReport, 0),
		Duplicates: make([]DuplicateReport, 0),
	}

	seenUUIDs := make(map[string]int) // uuid -> first observed row number

	// Process data rows
	for i := 1; i < len(records); i++ {
		rowNum := i + 1 // 1-based CSV line number (row 1 is header)
		row := records[i]

		// Skip entirely blank rows
		if isRowBlank(row) {
			continue
		}

		if urlCol >= len(row) {
			roster.BadRows = append(roster.BadRows, BadRowReport{
				RowNumber: rowNum,
				RawValue:  strings.Join(row, ","),
				Reason:    "missing profile URL column in row",
			})
			continue
		}

		rawURL := strings.TrimSpace(row[urlCol])
		if rawURL == "" {
			roster.BadRows = append(roster.BadRows, BadRowReport{
				RowNumber: rowNum,
				RawValue:  strings.Join(row, ","),
				Reason:    "empty profile URL",
			})
			continue
		}

		canonicalURL, uuid, err := NormalizeURL(rawURL)
		if err != nil {
			roster.BadRows = append(roster.BadRows, BadRowReport{
				RowNumber: rowNum,
				RawValue:  rawURL,
				Reason:    err.Error(),
			})
			continue
		}

		var name, email, team string
		if nameCol >= 0 && nameCol < len(row) {
			name = strings.TrimSpace(row[nameCol])
		}
		if emailCol >= 0 && emailCol < len(row) {
			email = strings.TrimSpace(row[emailCol])
		}
		if teamCol >= 0 && teamCol < len(row) {
			team = strings.TrimSpace(row[teamCol])
		}

		// Deduplicate by profile UUID
		if firstRow, exists := seenUUIDs[uuid]; exists {
			roster.Duplicates = append(roster.Duplicates, DuplicateReport{
				RowNumber:      rowNum,
				FirstRowNumber: firstRow,
				UUID:           uuid,
				Name:           name,
			})
			continue
		}

		seenUUIDs[uuid] = rowNum
		roster.Students = append(roster.Students, model.Student{
			ProfileURL: canonicalURL,
			UUID:       uuid,
			Name:       name,
			Email:      email,
			Team:       team,
			RowNumber:  rowNum,
		})
	}

	return roster, nil
}

func isRowBlank(row []string) bool {
	for _, field := range row {
		if strings.TrimSpace(field) != "" {
			return false
		}
	}
	return true
}

func detectURLColumn(records [][]string) int {
	if len(records) < 2 {
		return -1
	}

	colCount := len(records[0])
	matchCount := make([]int, colCount)

	for i := 1; i < len(records); i++ {
		row := records[i]
		for colIdx := 0; colIdx < colCount && colIdx < len(row); colIdx++ {
			val := strings.ToLower(strings.TrimSpace(row[colIdx]))
			if strings.Contains(val, "skills.google") || strings.Contains(val, "/public_profiles/") {
				matchCount[colIdx]++
			}
		}
	}

	bestCol := -1
	maxMatches := 0
	for colIdx, count := range matchCount {
		if count > maxMatches {
			maxMatches = count
			bestCol = colIdx
		}
	}

	return bestCol
}
