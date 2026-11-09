package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
)

func sampleResults() []model.ProfileResult {
	now := time.Now().UTC()
	return []model.ProfileResult{
		{
			Student: model.Student{
				Name:       "Alice Johnson",
				Email:      "alice@example.com",
				Team:       "Team Alpha",
				UUID:       "11111111-2222-3333-4444-555555555555",
				ProfileURL: "https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555",
			},
			DisplayName:       "Alice Johnson",
			Status:            model.StatusComplete,
			CountedBadges:     10,
			TotalBadges:       10,
			BadgeCount:        10,
			Points:            300,
			League:            "Silver League",
			LastBadgeEarnedAt: &now,
		},
		{
			Student: model.Student{
				Name:       "Bob Smith",
				Email:      "bob@example.com",
				UUID:       "22222222-3333-4444-5555-666666666666",
				ProfileURL: "https://www.skills.google/public_profiles/22222222-3333-4444-5555-666666666666",
			},
			Status:        model.StatusInProgress,
			CountedBadges: 4,
			TotalBadges:   4,
			BadgeCount:    4,
		},
		{
			Student: model.Student{
				Name: "Charlie Zero",
				UUID: "33333333-4444-5555-6666-777777777777",
			},
			Status:        model.StatusNoBadges,
			CountedBadges: 0,
		},
		{
			Student: model.Student{
				Name: "Private Student",
				UUID: "44444444-5555-6666-7777-888888888888",
			},
			Status: model.StatusPrivateOrNotFound,
		},
		{
			Student: model.Student{
				Name: "Error Student",
				UUID: "55555555-6666-7777-8888-999999999999",
			},
			Status: model.StatusNetworkError,
			Error:  "network timeout",
		},
	}
}

func TestJSONExport(t *testing.T) {
	results := sampleResults()
	var buf bytes.Buffer

	err := JSON(&buf, results, 10, false)
	if err != nil {
		t.Fatalf("JSON export failed: %v", err)
	}

	var payload ExportPayload
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal exported JSON failed: %v", err)
	}

	if payload.Summary.TotalStudents != 5 {
		t.Errorf("expected 5 students, got %d", payload.Summary.TotalStudents)
	}
	if payload.Summary.CompleteCount != 1 {
		t.Errorf("expected 1 complete, got %d", payload.Summary.CompleteCount)
	}
	if payload.Summary.InProgressCount != 1 {
		t.Errorf("expected 1 in_progress, got %d", payload.Summary.InProgressCount)
	}
	if payload.Summary.NoBadgesCount != 1 {
		t.Errorf("expected 1 no_badges, got %d", payload.Summary.NoBadgesCount)
	}
	if payload.Summary.PrivateOrNotFound != 1 {
		t.Errorf("expected 1 private_or_not_found, got %d", payload.Summary.PrivateOrNotFound)
	}
	if payload.Summary.ErrorCount != 1 {
		t.Errorf("expected 1 error, got %d", payload.Summary.ErrorCount)
	}
	if payload.Summary.Anonymized {
		t.Errorf("expected anonymized=false")
	}
	if len(payload.Results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(payload.Results))
	}
	if payload.Results[0].Student.Name != "Alice Johnson" {
		t.Errorf("expected Alice Johnson, got %s", payload.Results[0].Student.Name)
	}
}

func TestJSONExport_Anonymized(t *testing.T) {
	results := sampleResults()
	var buf bytes.Buffer

	err := JSON(&buf, results, 0, true)
	if err != nil {
		t.Fatalf("JSON anonymized export failed: %v", err)
	}

	var payload ExportPayload
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal exported JSON failed: %v", err)
	}

	if !payload.Summary.Anonymized {
		t.Errorf("expected summary.Anonymized=true")
	}

	for _, r := range payload.Results {
		if strings.Contains(r.Student.Name, "Alice") || strings.Contains(r.Student.Name, "Johnson") {
			t.Errorf("real name leaked in student name: %s", r.Student.Name)
		}
		if r.Student.Email != "" {
			t.Errorf("email not scrubbed: %s", r.Student.Email)
		}
		if strings.Contains(r.DisplayName, "Alice") {
			t.Errorf("real name leaked in display name: %s", r.DisplayName)
		}
	}
}

func TestCSVExport(t *testing.T) {
	results := sampleResults()
	var buf bytes.Buffer

	err := CSV(&buf, results, 10, false)
	if err != nil {
		t.Fatalf("CSV export failed: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("reading exported CSV failed: %v", err)
	}

	// 1 header row + 5 data rows = 6 rows
	if len(records) != 6 {
		t.Fatalf("expected 6 CSV rows, got %d", len(records))
	}

	header := records[0]
	if header[0] != "Name" || header[3] != "Status" || header[4] != "Counted Badges" {
		t.Errorf("unexpected header: %+v", header)
	}

	aliceRow := records[1]
	if aliceRow[0] != "Alice Johnson" || aliceRow[3] != "complete" || aliceRow[4] != "10" {
		t.Errorf("unexpected Alice row: %+v", aliceRow)
	}
}

func TestCSVExport_Anonymized(t *testing.T) {
	results := sampleResults()
	var buf bytes.Buffer

	err := CSV(&buf, results, 10, true)
	if err != nil {
		t.Fatalf("CSV anonymized export failed: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("reading exported CSV failed: %v", err)
	}

	aliceRow := records[1]
	if strings.Contains(aliceRow[0], "Alice Johnson") {
		t.Errorf("name leaked in anonymized CSV: %s", aliceRow[0])
	}
	if !strings.Contains(aliceRow[0], "A. J.") {
		t.Errorf("expected initials in anonymized CSV, got: %s", aliceRow[0])
	}
}

func TestAnonymizeName_EdgeCases(t *testing.T) {
	// Empty name
	anon := AnonymizeName("", "11111111-2222-3333-4444-555555555555")
	if !strings.HasPrefix(anon, "Participant") {
		t.Errorf("expected Participant prefix for empty name, got: %s", anon)
	}

	// Single word name
	anonSingle := AnonymizeName("Cher", "11111111-2222-3333-4444-555555555555")
	if !strings.HasPrefix(anonSingle, "C.") {
		t.Errorf("expected C. for single name, got: %s", anonSingle)
	}

	// Short UUID fallback
	anonShort := AnonymizeName("Shorty", "abc")
	if !strings.Contains(anonShort, "(abc)") {
		t.Errorf("expected (abc), got: %s", anonShort)
	}
}
