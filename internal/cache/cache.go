package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
)

const (
	DefaultTTL         = 6 * time.Hour
	DefaultNegativeTTL = 15 * time.Minute
)

// Entry represents a cached profile response.
type Entry struct {
	UUID      string       `json:"uuid"`
	HTML      []byte       `json:"html,omitempty"`
	Status    model.Status `json:"status"`
	FetchedAt time.Time    `json:"fetched_at"`
}

// IsExpired returns true if the entry was fetched more than ttl ago.
func (e *Entry) IsExpired(ttl time.Duration, now time.Time) bool {
	if ttl <= 0 {
		return false
	}
	return now.Sub(e.FetchedAt) > ttl
}

// Cache manages file-based caching for profile responses.
type Cache struct {
	dir         string
	ttl         time.Duration
	negativeTTL time.Duration
	now         func() time.Time
}

// Option allows customizing Cache configuration.
type Option func(*Cache)

// WithNow overrides the time provider for deterministic testing.
func WithNow(nowFn func() time.Time) Option {
	return func(c *Cache) {
		if nowFn != nil {
			c.now = nowFn
		}
	}
}

// WithNegativeTTL configures the cache expiration for PrivateOrNotFound profiles.
func WithNegativeTTL(d time.Duration) Option {
	return func(c *Cache) {
		if d > 0 {
			c.negativeTTL = d
		}
	}
}

// New creates or initializes an on-disk cache at dir.
func New(dir string, ttl time.Duration, opts ...Option) (*Cache, error) {
	if dir == "" {
		dir = ".badgeboard_cache"
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("creating cache directory %q: %w", dir, err)
	}

	c := &Cache{
		dir:         dir,
		ttl:         ttl,
		negativeTTL: DefaultNegativeTTL,
		now:         time.Now,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

func (c *Cache) filePath(uuid string) string {
	cleanUUID := strings.TrimSpace(uuid)
	return filepath.Join(c.dir, cleanUUID+".json")
}

// Get retrieves a cached profile entry if present and not expired according to its status TTL.
func (c *Cache) Get(uuid string) (*Entry, bool, error) {
	path := c.filePath(uuid)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading cache file %q: %w", path, err)
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false, nil
	}

	// Apply separate TTL based on status
	effectiveTTL := c.ttl
	if entry.Status == model.StatusPrivateOrNotFound {
		effectiveTTL = c.negativeTTL
	}

	if entry.IsExpired(effectiveTTL, c.now()) {
		return nil, false, nil
	}

	return &entry, true, nil
}

// Set writes an entry to disk using atomic rename.
// Rule: Cache ONLY successful 200 pages (Complete, InProgress, NoBadges) and PrivateOrNotFound.
// Never cache ParseError, RateLimited, NetworkError, or InvalidURL.
func (c *Cache) Set(uuid string, entry *Entry) error {
	if entry == nil {
		return nil
	}

	switch entry.Status {
	case model.StatusComplete, model.StatusInProgress, model.StatusNoBadges, model.StatusPrivateOrNotFound:
		// Allowed to be cached
	default:
		// Do not cache ParseError, RateLimited, NetworkError, etc.
		return nil
	}

	if entry.FetchedAt.IsZero() {
		entry.FetchedAt = c.now()
	}
	entry.UUID = uuid

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling cache entry: %w", err)
	}

	tmpFile, err := os.CreateTemp(c.dir, "entry-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp cache file: %w", err)
	}
	tmpName := tmpFile.Name()

	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("writing to temp cache file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp cache file: %w", err)
	}

	targetPath := c.filePath(uuid)
	if err := os.Rename(tmpName, targetPath); err != nil {
		_ = os.Remove(targetPath)
		if retryErr := os.Rename(tmpName, targetPath); retryErr != nil {
			return fmt.Errorf("renaming temp cache file to %q: %w", targetPath, retryErr)
		}
	}

	return nil
}
