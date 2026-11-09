package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Goal != 10 {
		t.Errorf("expected goal 10, got %d", cfg.Goal)
	}
	if cfg.RequiredBadges != nil {
		t.Errorf("expected nil RequiredBadges, got %v", cfg.RequiredBadges)
	}
	if cfg.Window != nil {
		t.Errorf("expected nil Window, got %v", cfg.Window)
	}
	if cfg.RateLimit.RPS != 1.0 {
		t.Errorf("expected RPS 1.0, got %f", cfg.RateLimit.RPS)
	}
	if cfg.RateLimit.Concurrency != 2 {
		t.Errorf("expected concurrency 2, got %d", cfg.RateLimit.Concurrency)
	}
	if cfg.Cache.TTLHours != 6 {
		t.Errorf("expected TTL 6, got %d", cfg.Cache.TTLHours)
	}

	// Default behavior should count every badge
	badge := model.BadgeRecord{
		Title:    "Random Badge",
		EarnedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if !cfg.IsBadgeEligible(badge) {
		t.Errorf("expected badge to be eligible under default config")
	}

	var nilCfg *Config
	if !nilCfg.IsBadgeEligible(badge) {
		t.Errorf("expected badge to be eligible with nil config")
	}
	if len(nilCfg.FilterEligibleBadges([]model.BadgeRecord{badge})) != 1 {
		t.Errorf("expected FilterEligibleBadges with nil config to return all badges")
	}
}

func TestLoad_NonExistentDefault(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error loading non-existent default: %v", err)
	}
	if cfg.Goal != 10 {
		t.Errorf("expected default goal 10, got %d", cfg.Goal)
	}
}

func TestLoad_Errors(t *testing.T) {
	// Non-existent explicit path
	_, err := Load("does_not_exist_file.toml")
	if err == nil {
		t.Errorf("expected error for non-existent explicit path")
	}

	// Invalid TOML content
	tmpDir := t.TempDir()
	badToml := filepath.Join(tmpDir, "bad.toml")
	if err := os.WriteFile(badToml, []byte("goal = [invalid toml syntax"), 0600); err != nil {
		t.Fatalf("failed to write bad toml: %v", err)
	}
	_, err = Load(badToml)
	if err == nil {
		t.Errorf("expected parse error for malformed toml")
	}
}

func TestLoad_CustomFileAndDefaults(t *testing.T) {
	tomlContent := `
goal = 0
required_badges = [
    "Explore Generative AI in Agent Platform",
    "Build a Website on Google Cloud"
]

[window]
start = 2026-10-01T00:00:00Z
end = 2026-11-30T23:59:59Z

[rate_limit]
rps = 0.0
concurrency = 0

[cache]
ttl_hours = 0
dir = ""
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "custom.toml")
	if err := os.WriteFile(configPath, []byte(tomlContent), 0600); err != nil {
		t.Fatalf("failed to write temp toml: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load custom config: %v", err)
	}

	// Verify zero values got set to defaults
	if cfg.Goal != DefaultGoal {
		t.Errorf("expected goal default %d, got %d", DefaultGoal, cfg.Goal)
	}
	if cfg.RateLimit.RPS != DefaultRPS {
		t.Errorf("expected rps default %f, got %f", DefaultRPS, cfg.RateLimit.RPS)
	}
	if cfg.RateLimit.Concurrency != DefaultConcurrency {
		t.Errorf("expected concurrency default %d, got %d", DefaultConcurrency, cfg.RateLimit.Concurrency)
	}
	if cfg.Cache.TTLHours != DefaultCacheTTL {
		t.Errorf("expected ttl default %d, got %d", DefaultCacheTTL, cfg.Cache.TTLHours)
	}
	if cfg.Cache.Dir != DefaultCacheDir {
		t.Errorf("expected dir default %s, got %s", DefaultCacheDir, cfg.Cache.Dir)
	}
}

func TestOptionalFeatures_Filtering(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 5, 23, 59, 59, 0, time.UTC)

	cfg := &Config{
		Goal: 10,
		RequiredBadges: []string{
			"Explore Generative AI in Agent Platform",
			"Build a Website on Google Cloud",
		},
		Window: &DateWindow{
			Start: &start,
			End:   &end,
		},
	}

	tests := []struct {
		name     string
		badge    model.BadgeRecord
		eligible bool
	}{
		{
			name: "matches title and within window",
			badge: model.BadgeRecord{
				Title:    "  explore generative ai in agent platform  ",
				EarnedAt: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC),
			},
			eligible: true,
		},
		{
			name: "matches title but before window",
			badge: model.BadgeRecord{
				Title:    "Explore Generative AI in Agent Platform",
				EarnedAt: time.Date(2026, 10, 31, 23, 59, 59, 0, time.UTC),
			},
			eligible: false,
		},
		{
			name: "matches title but after window",
			badge: model.BadgeRecord{
				Title:    "Build a Website on Google Cloud",
				EarnedAt: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC),
			},
			eligible: false,
		},
		{
			name: "within window but title not in required list",
			badge: model.BadgeRecord{
				Title:    "Using the Google Cloud Speech API",
				EarnedAt: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
			},
			eligible: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cfg.IsBadgeEligible(tc.badge)
			if got != tc.eligible {
				t.Errorf("IsBadgeEligible() = %v, want %v", got, tc.eligible)
			}
		})
	}
}


