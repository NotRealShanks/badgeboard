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
