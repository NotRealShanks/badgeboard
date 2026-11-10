package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NotRealShanks/badgeboard/internal/config"
	"github.com/NotRealShanks/badgeboard/internal/export"
	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/NotRealShanks/badgeboard/internal/roster"
)

func TestCLI_Version(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version cmd failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "badgeboard") {
		t.Errorf("expected version output to contain 'badgeboard', got: %s", out)
	}
}

func TestCLI_Version_Short(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"version", "--short"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version --short cmd failed: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != version {
		t.Errorf("expected version %q, got %q", version, out)
	}
}

func TestCLI_Demo(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "demo_results.json")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"demo", "--out", outPath, "--no-color"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("demo command failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading demo output failed: %v", err)
	}

	var payload export.ExportPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshaling demo output failed: %v", err)
	}

	if payload.Summary.TotalStudents != 15 {
		t.Errorf("expected 15 synthetic students, got %d", payload.Summary.TotalStudents)
	}
	if payload.Summary.CompleteCount != 4 {
		t.Errorf("expected 4 complete students, got %d", payload.Summary.CompleteCount)
	}
	if payload.Summary.InProgressCount != 5 {
		t.Errorf("expected 5 in-progress students, got %d", payload.Summary.InProgressCount)
	}
	if payload.Summary.NoBadgesCount != 2 {
		t.Errorf("expected 2 no-badges students, got %d", payload.Summary.NoBadgesCount)
	}
	if payload.Summary.PrivateOrNotFound != 2 {
		t.Errorf("expected 2 private/not-found students, got %d", payload.Summary.PrivateOrNotFound)
	}
}

func TestCLI_ExitCodes(t *testing.T) {
	// 1. Success exit code (0) for complete, in progress, no badges, and private/not found
	allEvaluated := []model.ProfileResult{
		{Status: model.StatusComplete},
		{Status: model.StatusInProgress},
		{Status: model.StatusNoBadges},
		{Status: model.StatusPrivateOrNotFound},
	}
	if code := determineExitCode(allEvaluated); code != ExitSuccess {
		t.Errorf("expected ExitSuccess (0) for all evaluated statuses including private, got %d", code)
	}

	// 2. Partial / Unreachable exit code (3) for network error, rate limit, parse error
	withNetworkError := []model.ProfileResult{
		{Status: model.StatusComplete},
		{Status: model.StatusNetworkError},
	}
	if code := determineExitCode(withNetworkError); code != ExitPartial {
		t.Errorf("expected ExitPartial (3) for network error, got %d", code)
	}

	withRateLimit := []model.ProfileResult{
		{Status: model.StatusRateLimited},
	}
	if code := determineExitCode(withRateLimit); code != ExitPartial {
		t.Errorf("expected ExitPartial (3) for rate limited, got %d", code)
	}

	withParseErr := []model.ProfileResult{
		{Status: model.StatusParseError},
	}
	if code := determineExitCode(withParseErr); code != ExitPartial {
		t.Errorf("expected ExitPartial (3) for parse error, got %d", code)
	}

	// 3. Exit 1 (Fatal error) test via missing file
	cmdFatal := newRootCmd()
	cmdFatal.SetArgs([]string{"run", "non_existent_roster_file.csv"})
	if err := cmdFatal.Execute(); err == nil {
		t.Errorf("expected fatal error for non-existent file, got nil")
	}

	// 4. Exit 2 (Usage error) test via invalid flags
	cmdUsage := newRootCmd()
	cmdUsage.SetArgs([]string{"run", "--unknown-flag-12345"})
	if err := cmdUsage.Execute(); err == nil {
		t.Errorf("expected usage error for unknown flag, got nil")
	}
}

func TestCLI_ContextCancellationGraceful(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	csvData := `Name,Google Skills Profile URL
Student One,https://www.skills.google/public_profiles/11111111-1111-1111-1111-111111111111
Student Two,https://www.skills.google/public_profiles/22222222-2222-2222-2222-222222222222
Student Three,https://www.skills.google/public_profiles/33333333-3333-3333-3333-333333333333
`
	rst, err := roster.Load(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("roster load failed: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.RateLimit.Concurrency = 1

	// Cancel context immediately
	cancel()

	trk, err := buildTracker(cfg, true, false)
	if err != nil {
		t.Fatalf("tracker build failed: %v", err)
	}

	results, err := trk.Track(ctx, rst, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results slots preserved on cancel, got %d", len(results))
	}

	for _, r := range results {
		if r.Status != model.StatusNetworkError {
			t.Errorf("expected StatusNetworkError on cancel, got: %s", r.Status)
		}
	}
}

func TestCLI_DeterministicDemoDataSpread(t *testing.T) {
	results := generateDeterministicDemoData(10)
	if len(results) != 15 {
		t.Fatalf("expected 15 results, got %d", len(results))
	}

	// Verify deterministic consistency across consecutive calls
	results2 := generateDeterministicDemoData(10)
	for i := range results {
		if results[i].Student.Name != results2[i].Student.Name {
			t.Errorf("demo data non-deterministic for item %d", i)
		}
		if results[i].CountedBadges != results2[i].CountedBadges {
			t.Errorf("badge count non-deterministic for item %d", i)
		}
	}
}
