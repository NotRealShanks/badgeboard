package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/pelletier/go-toml/v2"
)

// Default settings
const (
	DefaultGoal        = 10
	DefaultRPS         = 1.0
	DefaultConcurrency = 2
	DefaultCacheTTL    = 6
	DefaultCacheDir    = ".badgeboard_cache"
)

// DateWindow defines an optional campaign eligibility date window.
type DateWindow struct {
	Start *time.Time `toml:"start"`
	End   *time.Time `toml:"end"`
}

// RateLimitConfig defines rate-limiting options.
type RateLimitConfig struct {
	RPS         float64 `toml:"rps"`
	Concurrency int     `toml:"concurrency"`
}

// CacheConfig defines disk caching options.
type CacheConfig struct {
	TTLHours int    `toml:"ttl_hours"`
	Dir      string `toml:"dir"`
}

// Config represents the application configuration.
type Config struct {
	Goal           int             `toml:"goal"`
	RequiredBadges []string        `toml:"required_badges"`
	Window         *DateWindow     `toml:"window"`
	RateLimit      RateLimitConfig `toml:"rate_limit"`
	Cache          CacheConfig     `toml:"cache"`
}

// DefaultConfig returns the default configuration with all optional filters disabled.
func DefaultConfig() *Config {
	return &Config{
		Goal:           DefaultGoal,
		RequiredBadges: nil, // OFF by default
		Window:         nil, // OFF by default
		RateLimit: RateLimitConfig{
			RPS:         DefaultRPS,
			Concurrency: DefaultConcurrency,
		},
		Cache: CacheConfig{
			TTLHours: DefaultCacheTTL,
			Dir:      DefaultCacheDir,
		},
	}
}

// Load reads and parses a TOML configuration file.
// If path is empty, it attempts to load "badgeboard.toml" if present.
// If the default file is not found, DefaultConfig is returned without error.
// If an explicit path is provided and fails to load, an error is returned.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	var data []byte
	if path == "" {
		const defaultFilename = "badgeboard.toml"
		var err error
		data, err = os.ReadFile(defaultFilename)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return cfg, nil
			}
			return nil, fmt.Errorf("reading default config %q: %w", defaultFilename, err)
		}
	} else {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading config file %q: %w", path, err)
		}
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing TOML config: %w", err)
	}

	// Validate & ensure reasonable defaults
	if cfg.Goal <= 0 {
		cfg.Goal = DefaultGoal
	}
	if cfg.RateLimit.RPS <= 0 {
		cfg.RateLimit.RPS = DefaultRPS
	}
	if cfg.RateLimit.Concurrency <= 0 {
		cfg.RateLimit.Concurrency = DefaultConcurrency
	}
	if cfg.Cache.TTLHours <= 0 {
		cfg.Cache.TTLHours = DefaultCacheTTL
	}
	if cfg.Cache.Dir == "" {
		cfg.Cache.Dir = DefaultCacheDir
	}

	return cfg, nil
}

// IsBadgeEligible checks if a badge counts toward campaign progress based on optional filters.
// If required_badges is set, the badge title must match one of them (case-insensitive, trimmed).
// If window is set, the badge's earned time must fall within [start, end].
// By default (both OFF), every badge is eligible.
func (c *Config) IsBadgeEligible(badge model.BadgeRecord) bool {
	if c == nil {
		return true
	}

	// Optional check 1: Required badges list
	if len(c.RequiredBadges) > 0 {
		normalizedTitle := strings.ToLower(strings.TrimSpace(badge.Title))
		matched := false
		for _, req := range c.RequiredBadges {
			if strings.ToLower(strings.TrimSpace(req)) == normalizedTitle {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Optional check 2: Date window
	if c.Window != nil {
		if !badge.EarnedAt.IsZero() {
			if c.Window.Start != nil && badge.EarnedAt.Before(*c.Window.Start) {
				return false
			}
			if c.Window.End != nil && badge.EarnedAt.After(*c.Window.End) {
				return false
			}
		}
	}

	return true
}

// FilterEligibleBadges returns only the badges that satisfy campaign criteria.
func (c *Config) FilterEligibleBadges(badges []model.BadgeRecord) []model.BadgeRecord {
	if c == nil || (len(c.RequiredBadges) == 0 && c.Window == nil) {
		return badges
	}

	filtered := make([]model.BadgeRecord, 0, len(badges))
	for _, b := range badges {
		if c.IsBadgeEligible(b) {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

// EvaluateStatus determines the profile status given the count of eligible badges and target goal.
func (c *Config) EvaluateStatus(eligibleBadgeCount int) model.Status {
	goal := DefaultGoal
	if c != nil && c.Goal > 0 {
		goal = c.Goal
	}

	if eligibleBadgeCount >= goal {
		return model.StatusComplete
	}
	if eligibleBadgeCount > 0 {
		return model.StatusInProgress
	}
	return model.StatusNoBadges
}
