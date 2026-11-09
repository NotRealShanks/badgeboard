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

const DefaultTTL = 6 * time.Hour

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
	dir string
	ttl time.Duration
	now func() time.Time
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
		dir: dir,
		ttl: ttl,
		now: time.Now,
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

// Get retrieves a cached profile entry if present and not expired.
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
		// Corrupt cache file, treat as cache miss
		return nil, false, nil
	}

	if entry.IsExpired(c.ttl, c.now()) {
		// Expired entry
		return nil, false, nil
	}

	return &entry, true, nil
}

// Set writes an entry to disk using atomic rename.
// Transient statuses (rate_limited, network_error) are never cached.
func (c *Cache) Set(uuid string, entry *Entry) error {
	if entry == nil {
		return nil
	}

	// Rule: Do not cache transient failures
	if entry.Status == model.StatusRateLimited || entry.Status == model.StatusNetworkError {
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

	// Atomic write: write to temp file in same directory, then rename
	tmpFile, err := os.CreateTemp(c.dir, "entry-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp cache file: %w", err)
	}
	tmpName := tmpFile.Name()

	// Ensure cleanup in case of failure before rename
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
	// On Windows, os.Rename replaces destination if it exists in Go 1.5+
	if err := os.Rename(tmpName, targetPath); err != nil {
		// If rename fails (e.g. Windows file locking), remove target first and retry once
		_ = os.Remove(targetPath)
		if retryErr := os.Rename(tmpName, targetPath); retryErr != nil {
			return fmt.Errorf("renaming temp cache file to %q: %w", targetPath, retryErr)
		}
	}

	return nil
}
