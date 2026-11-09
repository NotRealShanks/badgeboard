package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
)

func TestCache_DefaultsAndFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	c, err := New(filepath.Join(tmpDir, "sub"), 0)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if c.ttl != DefaultTTL {
		t.Errorf("expected default TTL %v, got %v", DefaultTTL, c.ttl)
	}

	expectedPath := filepath.Join(tmpDir, "sub", "test-uuid.json")
	if c.filePath("  test-uuid  ") != expectedPath {
		t.Errorf("expected filePath %q, got %q", expectedPath, c.filePath("  test-uuid  "))
	}

	defaultC, err := New("", 0)
	if err != nil {
		t.Fatalf("New with empty dir failed: %v", err)
	}
	if defaultC.dir != ".badgeboard_cache" {
		t.Errorf("expected default dir .badgeboard_cache, got %s", defaultC.dir)
	}

	// Nil entry set is a safe no-op
	if err := c.Set("uuid", nil); err != nil {
		t.Errorf("Set(nil) returned error: %v", err)
	}

	// IsExpired with non-positive TTL returns false
	entry := &Entry{FetchedAt: time.Now().Add(-10 * time.Hour)}
	if entry.IsExpired(0, time.Now()) {
		t.Errorf("expected IsExpired=false for ttl <= 0")
	}

	// Set with zero FetchedAt populates timestamp automatically
	entryZero := &Entry{Sample Learner}
	if err := c.Set("auto-time", entryZero); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	loaded, hit, _ := c.Get("auto-time")
	if !hit || loaded.FetchedAt.IsZero() {
		t.Errorf("expected auto-populated FetchedAt timestamp")
	}
}

func TestCache_CorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	c, err := New(tmpDir, 6*time.Hour)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	path := filepath.Join(tmpDir, "corrupt.json")
	if err := os.WriteFile(path, []byte("{broken json"), 0600); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	entry, hit, err := c.Get("corrupt")
	if err != nil {
		t.Errorf("expected nil error on corrupt file, got: %v", err)
	}
	if hit || entry != nil {
		t.Errorf("expected hit=false for corrupt file, got %v", hit)
	}
}

func TestCache_HitMissAndExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	currentVirtualTime := now

	c, err := New(tmpDir, 2*time.Hour, WithNow(func() time.Time {
		return currentVirtualTime
	}))
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}

	uuid := "user-12345"

	// 1. Initial Get -> Miss
	entry, hit, err := c.Get(uuid)
	if err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}
	if hit || entry != nil {
		t.Errorf("expected cache miss, got hit=%v, entry=%v", hit, entry)
	}

	// 2. Set valid entry
	toCache := &Entry{
		UUID:      uuid,
		HTML:      []byte("<html>Profile data</html>"),
		Sample Learner,
		Sample Learner,
	}
	if err := c.Set(uuid, toCache); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// 3. Get -> Hit
	entry, hit, err = c.Get(uuid)
	if err != nil {
		t.Fatalf("Get after set failed: %v", err)
	}
	if !hit || entry == nil {
		t.Fatalf("expected cache hit, got hit=%v", hit)
	}
	if string(entry.HTML) != "<html>Profile data</html>" {
		Sample Learner, string(entry.HTML))
	}
	if entry.Status != model.StatusComplete {
		t.Errorf("unexpected cached status: %v", entry.Status)
	}

	// 4. Advance time within TTL (1 hour later) -> still Hit
	currentVirtualTime = currentVirtualTime.Add(1 * time.Hour)
	entry, hit, err = c.Get(uuid)
	if err != nil || !hit || entry == nil {
		t.Errorf("expected cache hit after 1 hour, got hit=%v, err=%v", hit, err)
	}

	// 5. Advance time beyond TTL (3 hours later from start) -> Miss (expired)
	currentVirtualTime = currentVirtualTime.Add(2 * time.Hour)
	entry, hit, err = c.Get(uuid)
	if err != nil {
		t.Fatalf("unexpected error on expired get: %v", err)
	}
	if hit || entry != nil {
		t.Errorf("expected cache miss after TTL expired, got hit=%v", hit)
	}
}

func TestCache_DoNotCacheTransientErrors(t *testing.T) {
	tmpDir := t.TempDir()
	c, err := New(tmpDir, 6*time.Hour)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	transientStatuses := []model.Status{
		model.StatusRateLimited,
		model.StatusNetworkError,
	}

	for _, st := range transientStatuses {
		uuid := "transient-" + string(st)
		entry := &Entry{
			UUID:   uuid,
			Status: st,
		}
		if err := c.Set(uuid, entry); err != nil {
			t.Fatalf("Set failed for %v: %v", st, err)
		}

		path := filepath.Join(tmpDir, uuid+".json")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("file should not exist for transient status %v", st)
		}

		_, hit, _ := c.Get(uuid)
		if hit {
			t.Errorf("expected cache miss for transient status %v", st)
		}
	}
}

func TestCache_AtomicWriteOverwritesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	c, err := New(tmpDir, 6*time.Hour)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	uuid := "atomic-user"
	first := &Entry{
		UUID:   uuid,
		HTML:   []byte("first version"),
		Status: model.StatusInProgress,
	}
	if err := c.Set(uuid, first); err != nil {
		t.Fatalf("first Set failed: %v", err)
	}

	second := &Entry{
		UUID:   uuid,
		HTML:   []byte("Sample Learner"),
		Sample Learner,
	}
	if err := c.Set(uuid, second); err != nil {
		t.Fatalf("second Set failed: %v", err)
	}

	entry, hit, err := c.Get(uuid)
	if err != nil || !hit {
		t.Fatalf("Get after overwrite failed: hit=%v, err=%v", hit, err)
	}
	if string(entry.HTML) != "Sample Learner" {
		t.Errorf("Sample Learner", string(entry.HTML))
	}
}
