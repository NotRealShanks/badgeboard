package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
)

func fixedDate() *time.Time {
	t := time.Date(2026, 11, 3, 14, 0, 0, 0, time.UTC)
	return &t
}

func mockResultsMixed() []model.ProfileResult {
	date := fixedDate()
	return []model.ProfileResult{
		{
			Student:           model.Student{Name: "Alice Johnson", UUID: "11111111-1111-1111-1111-111111111111"},
			Status:            model.StatusComplete,
			CountedBadges:     10,
			TotalBadges:       10,
			LastBadgeEarnedAt: date,
		},
		{
			Student:           model.Student{Name: "Bob Smith", UUID: "22222222-2222-2222-2222-222222222222"},
			Status:            model.StatusInProgress,
			CountedBadges:     7,
			TotalBadges:       7,
			LastBadgeEarnedAt: date,
		},
		{
			Student:       model.Student{Name: "Charlie Zero", UUID: "33333333-3333-3333-3333-333333333333"},
			Status:        model.StatusNoBadges,
			CountedBadges: 0,
		},
		{
			Student: model.Student{Name: "David Private", UUID: "44444444-4444-4444-4444-444444444444"},
			Status:  model.StatusPrivateOrNotFound,
			Error:   "profile private or not found",
		},
		{
			Student: model.Student{Name: "Eve Timeout", UUID: "55555555-5555-5555-5555-555555555555"},
			Status:  model.StatusNetworkError,
			Error:   "network timeout connecting to upstream",
		},
	}
}

func mockResultsAllComplete() []model.ProfileResult {
	date := fixedDate()
	return []model.ProfileResult{
		{
			Student:           model.Student{Name: "Grace Hopper", UUID: "66666666-6666-6666-6666-666666666666"},
			Status:            model.StatusComplete,
			CountedBadges:     10,
			TotalBadges:       10,
			LastBadgeEarnedAt: date,
		},
		{
			Student:           model.Student{Name: "Ada Lovelace", UUID: "77777777-7777-7777-7777-777777777777"},
			Status:            model.StatusComplete,
			CountedBadges:     10,
			TotalBadges:       10,
			LastBadgeEarnedAt: date,
		},
	}
}

func compareOrUpdateGolden(t *testing.T, goldenName, actual string) {
	t.Helper()
	goldenPath := filepath.Join("testdata", goldenName)

	cleanActual := strings.ReplaceAll(StripANSI(actual), "\r\n", "\n")

	// If UPDATE_GOLDEN=1, write expected file
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		_ = os.MkdirAll("testdata", 0755)
		if err := os.WriteFile(goldenPath, []byte(cleanActual), 0644); err != nil {
			t.Fatalf("failed to update golden file %s: %v", goldenPath, err)
		}
		return
	}

	expectedBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file %s failed: %v (run with UPDATE_GOLDEN=1 to create)", goldenPath, err)
	}

	cleanExpected := strings.ReplaceAll(string(expectedBytes), "\r\n", "\n")
	if cleanActual != cleanExpected {
		t.Errorf("Golden mismatch for %s:\n--- EXPECTED ---\n%s\n--- ACTUAL ---\n%s", goldenName, cleanExpected, cleanActual)
	}
}

func TestTable_Golden_Mixed(t *testing.T) {
	results := mockResultsMixed()
	opts := TableOptions{
		Goal:      10,
		NoColor:   true,
		Width:     90,
		CacheHits: 2,
		BadRows:   1,
		Duration:  2500 * time.Millisecond,
	}

	rendered := RenderFinalTable(results, opts)
	compareOrUpdateGolden(t, "mixed.golden", rendered)
}

func TestTable_Golden_AllComplete(t *testing.T) {
	results := mockResultsAllComplete()
	opts := TableOptions{
		Goal:      10,
		NoColor:   true,
		Width:     90,
		CacheHits: 2,
		BadRows:   0,
		Duration:  1200 * time.Millisecond,
	}

	rendered := RenderFinalTable(results, opts)
	compareOrUpdateGolden(t, "all_complete.golden", rendered)
}

func TestTable_Golden_Empty(t *testing.T) {
	var results []model.ProfileResult
	opts := TableOptions{
		Goal:      10,
		NoColor:   true,
		Width:     90,
		CacheHits: 0,
		BadRows:   0,
		Duration:  100 * time.Millisecond,
	}

	rendered := RenderFinalTable(results, opts)
	compareOrUpdateGolden(t, "empty.golden", rendered)
}

func TestTable_Golden_Narrow80(t *testing.T) {
	results := mockResultsMixed()
	opts := TableOptions{
		Goal:      10,
		NoColor:   true,
		Width:     80,
		CacheHits: 1,
		BadRows:   0,
		Duration:  1500 * time.Millisecond,
	}

	rendered := RenderFinalTable(results, opts)
	compareOrUpdateGolden(t, "narrow_80.golden", rendered)
}
