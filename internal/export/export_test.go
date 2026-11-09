package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/NotRealShanks/badgeboard/internal/parse"
	"github.com/NotRealShanks/badgeboard/internal/roster"
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
		if r.Student.ProfileURL != "" {
			t.Errorf("profile URL not scrubbed in anonymized student: %s", r.Student.ProfileURL)
		}
		if r.Student.UUID != "" {
			t.Errorf("uuid not scrubbed in anonymized student: %s", r.Student.UUID)
		}
		for _, b := range r.Badges {
			if b.URL != "" {
				t.Errorf("badge URL not scrubbed in anonymized badge: %s", b.URL)
			}
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
	if aliceRow[1] != "" {
		t.Errorf("profile URL must be empty in anonymized CSV, got %q", aliceRow[1])
	}
	if aliceRow[2] != "" {
		t.Errorf("UUID must be empty in anonymized CSV, got %q", aliceRow[2])
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

func TestAnonymize_RealSampleAudit(t *testing.T) {
	sampleHTML, err := os.ReadFile("../../testdata/sample_profile.html")
	if err != nil {
		t.Fatalf("failed reading sample profile HTML: %v", err)
	}

	parsed, err := parse.Parse(sampleHTML)
	if err != nil {
		t.Fatalf("parse real sample failed: %v", err)
	}

	fullUUID := "be556f7b-ab4b-4be7-82fb-9be45202ddd7"
	fullURL := "https://www.skills.google/public_profiles/" + fullUUID
	rosterName := "Sample Learnereloper"
	email := "litmus.dev@example.com"
	h1DisplayName := "Sample Learner"

	totalBadges := len(parsed.Badges)
	res := model.ProfileResult{
		Student: model.Student{
			Name:       rosterName,
			Email:      email,
			Team:       "Core Team",
			UUID:       fullUUID,
			ProfileURL: fullURL,
		},
		DisplayName:   parsed.DisplayName,
		Status:        model.EvaluateStatus(totalBadges, 10, totalBadges, parsed.IsPrivate),
		CountedBadges: totalBadges,
		TotalBadges:   totalBadges,
		Badges:        parsed.Badges,
		Points:        parsed.Points,
		League:        parsed.League,
	}

	// Assert preconditions
	if res.DisplayName != h1DisplayName {
		t.Fatalf("Sample Learner", h1DisplayName, res.DisplayName)
	}
	if len(res.Badges) != 10 {
		t.Fatalf("expected 10 badges, got %d", len(res.Badges))
	}

	// 1. Check JSON anonymized export
	var jsonBuf bytes.Buffer
	if err := JSON(&jsonBuf, []model.ProfileResult{res}, 10, true); err != nil {
		t.Fatalf("JSON export failed: %v", err)
	}
	jsonOut := jsonBuf.String()

	// Forbidden strings that must NEVER appear in anonymized output
	forbiddenStrings := []string{
		fullUUID,
		fullURL,
		h1DisplayName,
		rosterName,
		email,
		"/badges/",
		"public_profiles/" + fullUUID,
	}

	for _, s := range forbiddenStrings {
		if strings.Contains(jsonOut, s) {
			t.Errorf("Anonymized JSON audit failed: found forbidden string %q in output", s)
		}
	}

	// 2. Check CSV anonymized export
	var csvBuf bytes.Buffer
	if err := CSV(&csvBuf, []model.ProfileResult{res}, 10, true); err != nil {
		t.Fatalf("CSV export failed: %v", err)
	}
	csvOut := csvBuf.String()

	for _, s := range forbiddenStrings {
		if strings.Contains(csvOut, s) {
			t.Errorf("Anonymized CSV audit failed: found forbidden string %q in output", s)
		}
	}
}

func TestCSVExport_FormulaInjectionNeutralized(t *testing.T) {
	maliciousResults := []model.ProfileResult{
		{
			Student: model.Student{
				Name:       "=cmd|' /C calc'!A0",
				ProfileURL: "+https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555",
				UUID:       "-11111111-2222-3333-4444-555555555555",
			},
			DisplayName: "@Admin",
			Status:      model.StatusComplete,
			League:      "\tTabLeague",
			Error:       "\rCarriageReturnError",
		},
	}

	var buf bytes.Buffer
	if err := CSV(&buf, maliciousResults, 10, false); err != nil {
		t.Fatalf("CSV export failed: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("reading CSV failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	row := records[1]
	// row[0] is Name: was "=cmd|' /C calc'!A0" -> must become "'=cmd|' /C calc'!A0"
	if row[0] != "'=cmd|' /C calc'!A0" {
		t.Errorf("expected neutralized name, got %q", row[0])
	}
	// row[1] is ProfileURL: was "+https..." -> must become "'+https..."
	if row[1] != "'+https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555" {
		t.Errorf("expected neutralized URL, got %q", row[1])
	}
	// row[2] is UUID: was "-1111..." -> must become "'-1111..."
	if row[2] != "'-11111111-2222-3333-4444-555555555555" {
		t.Errorf("expected neutralized UUID, got %q", row[2])
	}
	// row[8] is League: was "\tTabLeague" -> must become "'\tTabLeague"
	if row[8] != "'\tTabLeague" {
		t.Errorf("expected neutralized League, got %q", row[8])
	}
	// row[10] is Error: was "\rCarriageReturnError" -> must become "'\rCarriageReturnError"
	if row[10] != "'\rCarriageReturnError" {
		t.Errorf("expected neutralized Error, got %q", row[10])
	}
}

func TestAnonymize_FailurePathAndBadRowsAudit(t *testing.T) {
	failureUUID1 := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	failureURL1 := "https://www.skills.google/public_profiles/" + failureUUID1
	name1 := "Fail Student One"
	email1 := "fail1@example.com"

	failureUUID2 := "12345678-1234-1234-1234-123456789abc"
	failureURL2 := "https://www.skills.google/public_profiles/" + failureUUID2
	name2 := "Fail Student Two"
	email2 := "fail2@example.com"

	results := []model.ProfileResult{
		{
			Student: model.Student{
				Name:       name1,
				Email:      email1,
				UUID:       failureUUID1,
				ProfileURL: failureURL1,
			},
			Status: model.StatusPrivateOrNotFound,
			Error:  "profile not found or private for Fail Student One (aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee) at https://www.skills.google/public_profiles/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		},
		{
			Student: model.Student{
				Name:       name2,
				Email:      email2,
				UUID:       failureUUID2,
				ProfileURL: failureURL2,
			},
			Status: model.StatusRateLimited,
			Error:  "rate limited when contacting https://www.skills.google/public_profiles/12345678-1234-1234-1234-123456789abc for student Fail Student Two <fail2@example.com>",
		},
		{
			Student: model.Student{
				Name:       "Fail Student Three",
				Email:      "fail3@example.com",
				UUID:       "99999999-8888-7777-6666-555555555555",
				ProfileURL: "https://www.skills.google/public_profiles/99999999-8888-7777-6666-555555555555",
			},
			Status: model.StatusNetworkError,
			Error:  "network timeout dialing https://www.skills.google/public_profiles/99999999-8888-7777-6666-555555555555",
		},
	}

	badRows := []roster.BadRowReport{
		{
			RowNumber: 42,
			RawValue:  "John Leaky,https://www.skills.google/public_profiles/badbad00-1111-2222-3333-444444444444,leaky@example.com",
			Reason:    "malformed UUID on https://www.skills.google/public_profiles/badbad00-1111-2222-3333-444444444444",
		},
	}

	// 1. JSON anonymized test
	var jsonBuf bytes.Buffer
	if err := JSONWithBadRows(&jsonBuf, results, badRows, 10, true); err != nil {
		t.Fatalf("JSONWithBadRows failed: %v", err)
	}
	jsonOut := jsonBuf.String()

	forbidden := []string{
		failureUUID1, failureURL1, name1, email1,
		failureUUID2, failureURL2, name2, email2,
		"99999999-8888-7777-6666-555555555555",
		"John Leaky", "leaky@example.com", "badbad00-1111-2222-3333-444444444444",
	}

	for _, str := range forbidden {
		if strings.Contains(jsonOut, str) {
			t.Errorf("Anonymized JSON audit failed: found forbidden string %q in JSON output", str)
		}
	}

	// 2. CSV anonymized test
	var csvBuf bytes.Buffer
	if err := CSV(&csvBuf, results, 10, true); err != nil {
		t.Fatalf("CSV failed: %v", err)
	}
	csvOut := csvBuf.String()

	csvForbidden := []string{
		failureUUID1, failureURL1, name1, email1,
		failureUUID2, failureURL2, name2, email2,
		"99999999-8888-7777-6666-555555555555",
	}

	for _, str := range csvForbidden {
		if strings.Contains(csvOut, str) {
			t.Errorf("Anonymized CSV audit failed: found forbidden string %q in CSV output", str)
		}
	}
}

