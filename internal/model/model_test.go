package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStatus_StringAndJSON(t *testing.T) {
	statuses := []Status{
		StatusComplete,
		StatusInProgress,
		StatusNoBadges,
		StatusPrivateOrNotFound,
		StatusInvalidURL,
		StatusRateLimited,
		StatusNetworkError,
		StatusParseError,
	}

	for _, st := range statuses {
		t.Run(string(st), func(t *testing.T) {
			if st.String() != string(st) {
				t.Errorf("expected %q, got %q", string(st), st.String())
			}

			data, err := json.Marshal(st)
			if err != nil {
				t.Fatalf("MarshalJSON failed: %v", err)
			}

			var unmarshaled Status
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("UnmarshalJSON failed: %v", err)
			}

			if unmarshaled != st {
				t.Errorf("expected %q after roundtrip, got %q", st, unmarshaled)
			}
		})
	}
}

func TestProfileResult_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	earned := now.Add(-24 * time.Hour)

	res := ProfileResult{
		Student: Student{
			ProfileURL: "https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555",
			UUID:       "11111111-2222-3333-4444-555555555555",
			Name:       "Test User",
			Email:      "test@example.com",
			Team:       "Team Alpha",
			RowNumber:  1,
		},
		Status:      StatusComplete,
		DisplayName: "Test User Public",
		League:      "Silver League",
		Points:      339,
		Badges: []BadgeRecord{
			{
				ID:        "12345",
				Title:     "Generative AI",
				EarnedAt:  earned,
				RawEarned: "Earned Nov  3, 2026 EST",
				URL:       "https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555/badges/12345",
				ImageURL:  "https://cdn.qwiklabs.com/image.png",
			},
		},
		BadgeCount:        1,
		LastBadgeEarnedAt: &earned,
		FetchedAt:         now,
		FromCache:         false,
	}

	bytes, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ProfileResult
	if err := json.Unmarshal(bytes, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Student.UUID != res.Student.UUID {
		t.Errorf("UUID mismatch: got %s, want %s", decoded.Student.UUID, res.Student.UUID)
	}
	if decoded.Status != StatusComplete {
		t.Errorf("Status mismatch: got %v, want %v", decoded.Status, StatusComplete)
	}
	if len(decoded.Badges) != 1 || decoded.Badges[0].ID != "12345" {
		t.Errorf("Badges mismatch: %+v", decoded.Badges)
	}
}
