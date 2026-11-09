package roster

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantURL      string
		wantUUID     string
		expectErr    bool
	}{
		{
			name:      "canonical skills.google URL",
			input:     "https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7",
			wantURL:   "https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7",
			wantUUID:  "be556f7b-ab4b-4be7-82fb-9be45202ddd7",
			expectErr: false,
		},
		{
			name:      "URL with query params, fragment and trailing slash",
			input:     "https://skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7/?locale=es#badges",
			wantURL:   "https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7",
			wantUUID:  "be556f7b-ab4b-4be7-82fb-9be45202ddd7",
			expectErr: false,
		},
		{
			name:      "cloudskillsboost.google domain mapped to canonical shape",
			input:     "https://cloudskillsboost.google/public_profiles/11111111-2222-3333-4444-555555555555",
			wantURL:   "https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555",
			wantUUID:  "11111111-2222-3333-4444-555555555555",
			expectErr: false,
		},
		{
			name:      "URL without scheme (auto-prepends https)",
			input:     "www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7",
			wantURL:   "https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7",
			wantUUID:  "be556f7b-ab4b-4be7-82fb-9be45202ddd7",
			expectErr: false,
		},
		{
			name:      "untrusted domain",
			input:     "https://evil.com/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7",
			expectErr: true,
		},
		{
			name:      "invalid path structure",
			input:     "https://www.skills.google/other_path/be556f7b-ab4b-4be7-82fb-9be45202ddd7",
			expectErr: true,
		},
		{
			name:      "malformed UUID",
			input:     "https://www.skills.google/public_profiles/not-a-valid-uuid",
			expectErr: true,
		},
		{
			name:      "empty URL",
			input:     "   ",
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotUUID, err := NormalizeURL(tc.input)
			if (err != nil) != tc.expectErr {
				t.Fatalf("NormalizeURL(%q) err = %v, expectErr = %v", tc.input, err, tc.expectErr)
			}
			if !tc.expectErr {
				if gotURL != tc.wantURL {
					t.Errorf("gotURL = %q, want %q", gotURL, tc.wantURL)
				}
				if gotUUID != tc.wantUUID {
					t.Errorf("gotUUID = %q, want %q", gotUUID, tc.wantUUID)
				}
			}
		})
	}
}

func TestLoad_SampleRosterFileWithBOM(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "sample_roster.csv")
	roster, err := LoadFile(path)
	if err != nil {
		t.Fatalf("failed to load sample roster file: %v", err)
	}

	// 1. Verify valid students parsed (expected 6 distinct valid students)
	if len(roster.Students) != 6 {
		t.Fatalf("expected 6 valid students, got %d", len(roster.Students))
	}

	// 2. Verify duplicate detected (Frank Castle row has same UUID as Bob Smith)
	if len(roster.Duplicates) != 1 {
		t.Fatalf("expected 1 duplicate report, got %d", len(roster.Duplicates))
	}
	if roster.Duplicates[0].UUID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("unexpected duplicate UUID: %s", roster.Duplicates[0].UUID)
	}

	// 3. Verify bad row detected (Evan Wright has invalid host example.com)
	if len(roster.BadRows) != 1 {
		t.Fatalf("expected 1 bad row report, got %d", len(roster.BadRows))
	}
	if !strings.Contains(roster.BadRows[0].Reason, "unsupported host") {
		t.Errorf("unexpected bad row reason: %s", roster.BadRows[0].Reason)
	}

	// 4. Verify optional column handling: Alice Johnson has name, email, team
	alice := roster.Students[0]
	if alice.Name != "Alice Johnson" || alice.Email != "alice@example.com" || alice.Team != "Team Alpha" {
		t.Errorf("Alice data mismatch: %+v", alice)
	}

	// 5. Verify row without name parses cleanly (last row has empty name)
	noNameStudent := roster.Students[len(roster.Students)-1]
	if noNameStudent.Name != "" {
		t.Errorf("expected empty name for student without name, got: %q", noNameStudent.Name)
	}
	if noNameStudent.Email != "no-name@example.com" {
		t.Errorf("expected email preserved, got: %s", noNameStudent.Email)
	}
}

func TestLoad_AutoDetectColumn(t *testing.T) {
	// CSV without "Google Skills Profile URL" header, but column 1 contains profile URLs
	csvData := `ID,URL_Link,Full Name
1,https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7,Test Auto
2,https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555,Test Auto 2
`
	roster, err := Load(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("Load with auto-detect failed: %v", err)
	}

	if len(roster.Students) != 2 {
		t.Fatalf("expected 2 students from auto-detected column, got %d", len(roster.Students))
	}
	if roster.Students[0].Name != "Test Auto" {
		t.Errorf("expected name Test Auto, got %s", roster.Students[0].Name)
	}
}

func TestLoad_MissingHeaderAndNoMatch(t *testing.T) {
	csvData := `Name,Age,City
Alice,30,New York
Bob,25,San Francisco
`
	_, err := Load(strings.NewReader(csvData))
	if err == nil {
		t.Errorf("expected ErrHeaderNotFound, got nil")
	}
	if !strings.Contains(err.Error(), "header not found") {
		t.Errorf("expected header not found error, got: %v", err)
	}
}

func TestLoad_EmptyCSV(t *testing.T) {
	_, err := Load(strings.NewReader(""))
	if err == nil {
		t.Errorf("expected ErrEmptyRoster, got nil")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := LoadFile("non-existent-roster-file.csv")
	if err == nil {
		t.Errorf("expected error for non-existent file, got nil")
	}
}

func TestLoad_DeduplicateDifferentCaseUUID(t *testing.T) {
	csvData := `Name,Google Skills Profile URL
Student One,https://www.skills.google/public_profiles/BE556F7B-AB4B-4BE7-82FB-9BE45202DDD7
Student Two,https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7
`
	roster, err := Load(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("unexpected error loading roster: %v", err)
	}

	if len(roster.Students) != 1 {
		t.Fatalf("expected 1 unique student, got %d", len(roster.Students))
	}
	if len(roster.Duplicates) != 1 {
		t.Fatalf("expected 1 duplicate report, got %d", len(roster.Duplicates))
	}
	if roster.Students[0].UUID != "be556f7b-ab4b-4be7-82fb-9be45202ddd7" {
		t.Errorf("expected lowercase UUID, got %q", roster.Students[0].UUID)
	}
	if roster.Duplicates[0].UUID != "be556f7b-ab4b-4be7-82fb-9be45202ddd7" {
		t.Errorf("expected duplicate report UUID to be lowercase, got %q", roster.Duplicates[0].UUID)
	}
}

func TestLoad_SingleColumnCRLF(t *testing.T) {
	csvData := "Google Skills Profile URL\r\n" +
		"https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7\r\n" +
		"https://www.skills.google/public_profiles/8f0d00c3-7674-4042-ba15-d26a3fae84f9\r\n"

	rst, err := Load(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("unexpected error loading single column CRLF: %v", err)
	}

	if len(rst.Students) != 2 {
		t.Fatalf("expected 2 students, got %d", len(rst.Students))
	}

	for _, s := range rst.Students {
		if strings.ContainsAny(s.ProfileURL, "\r\n\t ") {
			t.Errorf("ProfileURL contains whitespace or CR/LF: %q", s.ProfileURL)
		}
		if strings.ContainsAny(s.UUID, "\r\n\t ") {
			t.Errorf("UUID contains whitespace or CR/LF: %q", s.UUID)
		}
	}
}

func TestLoad_SingleColumnCRLF_NoTrailingNewline(t *testing.T) {
	csvData := "Google Skills Profile URL\r\n" +
		"https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7\r\n" +
		"https://www.skills.google/public_profiles/8f0d00c3-7674-4042-ba15-d26a3fae84f9"

	rst, err := Load(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("unexpected error loading CRLF with no trailing newline: %v", err)
	}

	if len(rst.Students) != 2 {
		t.Fatalf("expected 2 students, got %d", len(rst.Students))
	}
	if rst.Students[1].UUID != "8f0d00c3-7674-4042-ba15-d26a3fae84f9" {
		t.Errorf("expected UUID 8f0d00c3-7674-4042-ba15-d26a3fae84f9, got %q", rst.Students[1].UUID)
	}
}

func TestLoad_HeaderOnly(t *testing.T) {
	csvData := "Google Skills Profile URL\r\n"
	rst, err := Load(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("expected nil error for header-only CSV, got: %v", err)
	}
	if len(rst.Students) != 0 {
		t.Errorf("expected 0 students, got %d", len(rst.Students))
	}
}

func TestLoad_CompletelyEmptyFile(t *testing.T) {
	_, err := Load(strings.NewReader(""))
	if err == nil {
		t.Fatalf("expected error for empty file, got nil")
	}
	if err != ErrEmptyRoster {
		t.Errorf("expected ErrEmptyRoster, got %v", err)
	}
}

func TestLoad_OrganizerSample_WithAndWithoutInfoColumn(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "organizer_sample.csv")
	rstWithInfo, err := LoadFile(path)
	if err != nil {
		t.Fatalf("failed loading organizer sample: %v", err)
	}

	// Create single column version without [INFO]
	var singleColLines []string
	singleColLines = append(singleColLines, "Google Skills Profile URL")
	for _, s := range rstWithInfo.Students {
		singleColLines = append(singleColLines, s.ProfileURL)
	}
	singleColCSV := strings.Join(singleColLines, "\r\n") + "\r\n"

	rstSingleCol, err := Load(strings.NewReader(singleColCSV))
	if err != nil {
		t.Fatalf("failed loading single column version: %v", err)
	}

	if len(rstWithInfo.Students) != len(rstSingleCol.Students) {
		t.Fatalf("student count mismatch: with info=%d, single col=%d",
			len(rstWithInfo.Students), len(rstSingleCol.Students))
	}

	for i := range rstWithInfo.Students {
		s1 := rstWithInfo.Students[i]
		s2 := rstSingleCol.Students[i]
		if s1.UUID != s2.UUID || s1.ProfileURL != s2.ProfileURL || s1.Name != s2.Name {
			t.Errorf("student mismatch at index %d:\nwith info: %+v\nsingle col: %+v", i, s1, s2)
		}
	}
}

func TestLoad_URLColumnNotFirst(t *testing.T) {
	csvData := "Notes,Age,Google Skills Profile URL,Role\r\n" +
		"Candidate 1,22,https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7,Engineer\r\n" +
		"Candidate 2,24,https://www.skills.google/public_profiles/8f0d00c3-7674-4042-ba15-d26a3fae84f9,Designer\r\n"

	rst, err := Load(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("unexpected error loading CSV with URL not first: %v", err)
	}

	if len(rst.Students) != 2 {
		t.Fatalf("expected 2 students, got %d", len(rst.Students))
	}
	if rst.Students[0].UUID != "be556f7b-ab4b-4be7-82fb-9be45202ddd7" {
		t.Errorf("student 0 UUID mismatch: %q", rst.Students[0].UUID)
	}
	if rst.Students[1].UUID != "8f0d00c3-7674-4042-ba15-d26a3fae84f9" {
		t.Errorf("student 1 UUID mismatch: %q", rst.Students[1].UUID)
	}
}

func TestNormalizeURL_WhitespaceAndCarriageReturn(t *testing.T) {
	raw := "\r\n\t https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7 \t\r\n"
	normURL, uuid, err := NormalizeURL(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.ContainsAny(normURL, "\r\n\t ") {
		t.Errorf("normURL contains whitespace or CR/LF: %q", normURL)
	}
	if strings.ContainsAny(uuid, "\r\n\t ") {
		t.Errorf("uuid contains whitespace or CR/LF: %q", uuid)
	}
	if normURL != "https://www.skills.google/public_profiles/be556f7b-ab4b-4be7-82fb-9be45202ddd7" {
		t.Errorf("expected canonical URL, got %q", normURL)
	}
	if uuid != "be556f7b-ab4b-4be7-82fb-9be45202ddd7" {
		t.Errorf("expected canonical UUID, got %q", uuid)
	}
}


