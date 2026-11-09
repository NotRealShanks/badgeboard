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
	c, err := New(filepath.Join(tmpDir, "sub"), 0, WithNegativeTTL(0))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if c.ttl != DefaultTTL {
		t.Errorf("expected default TTL %v, got %v", DefaultTTL, c.ttl)
	}
	if c.negativeTTL != DefaultNegativeTTL {
		t.Errorf("expected default negative TTL %v, got %v", DefaultNegativeTTL, c.negativeTTL)
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

	if err := c.Set("uuid", nil); err != nil {
		t.Errorf("Set(nil) returned error: %v", err)
	}

	entry := &Entry{FetchedAt: time.Now().Add(-10 * time.Hour)}
	if entry.IsExpired(0, time.Now()) {
		t.Errorf("expected IsExpired=false for ttl <= 0")
	}

	entryZero := &Entry{Sample Learner}
	if err := c.Set("auto-time", entryZero); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	loaded, hit, _ := c.Get("auto-time")
	if !hit || loaded.FetchedAt.IsZero() {
		t.Errorf("expected auto-populated FetchedAt timestamp")
	}
}

func TestCache_Standard200_ExpiresAfter6Hours(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	fakeNow := func() time.Time { return now }

	c, err := New(tmpDir, 6*time.Hour, WithNow(fakeNow))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	entry := &Entry{
		Sample Learner,
		Sample Learner,
		HTML:   []byte("Sample Learner"),
	}

	if err := c.Set("200-uuid", entry); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// 1. Immediately after set -> cache hit
	cached, hit, err := c.Get("200-uuid")
	if err != nil || !hit || cached == nil {
		t.Fatalf("expected immediate cache hit, got hit=%v, err=%v", hit, err)
	}
	if string(cached.HTML) != "Sample Learner" {
		Sample Learner, string(cached.HTML))
	}

	// 2. Advance clock 5 hours 59 minutes -> still valid hit
	now = now.Add(5*time.Hour + 59*time.Minute)
	cached, hit, err = c.Get("200-uuid")
	if err != nil || !hit || cached == nil {
		t.Fatalf("expected cache hit at 5h59m, got hit=%v, err=%v", hit, err)
	}

	// 3. Advance clock past 6 hours (6 hours 1 minute) -> expired
	now = now.Add(2 * time.Minute) // now at 6h 1m
	cached, hit, err = c.Get("200-uuid")
	if err != nil {
		t.Fatalf("unexpected error on expired get: %v", err)
	}
	if hit {
		t.Errorf("expected cache miss (expired) at 6h1m, got hit=true")
	}
	if cached != nil {
		t.Errorf("expected nil cached entry on expiration, got %+v", cached)
	}
}


func TestCache_DoNotCacheErrors(t *testing.T) {
	tmpDir := t.TempDir()
	c, err := New(tmpDir, 6*time.Hour)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	uncachedStatuses := []model.Status{
		model.StatusParseError,
		model.StatusRateLimited,
		model.StatusNetworkError,
		model.StatusInvalidURL,
	}

	for _, st := range uncachedStatuses {
		uuid := "err-" + string(st)
		entry := &Entry{
			UUID:   uuid,
			Status: st,
		}
		if err := c.Set(uuid, entry); err != nil {
			t.Fatalf("Set failed for %v: %v", st, err)
		}

		path := filepath.Join(tmpDir, uuid+".json")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("file should not exist for uncacheable status %v", st)
		}

		_, hit, _ := c.Get(uuid)
		if hit {
			t.Errorf("expected cache miss for uncacheable status %v", st)
		}
	}
}

func TestCache_PrivateOrNotFound_ShortNegativeTTL(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	currentVirtualTime := now

	negativeTTL := 15 * time.Minute
	standardTTL := 6 * time.Hour

	c, err := New(tmpDir, standardTTL,
		WithNegativeTTL(negativeTTL),
		WithNow(func() time.Time { return currentVirtualTime }),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	privateUUID := "private-user"
	validUUID := "valid-user"

	_ = c.Set(privateUUID, &Entry{
		UUID:      privateUUID,
		Status:    model.StatusPrivateOrNotFound,
		Sample Learner,
	})

	_ = c.Set(validUUID, &Entry{
		Sample Learner,
		HTML:      []byte("Sample Learner"),
		Sample Learner,
		Sample Learner,
	})

	// 10 minutes later: both should still be hit
	currentVirtualTime = currentVirtualTime.Add(10 * time.Minute)
	_, privHit, _ := c.Get(privateUUID)
	_, validHit, _ := c.Get(validUUID)
	if !privHit || !validHit {
		t.Errorf("expected both hit at 10m: priv=%v, valid=%v", privHit, validHit)
	}

	// 20 minutes later: PrivateOrNotFound should be EXPIRED (negativeTTL=15m), valid profile still hit
	currentVirtualTime = currentVirtualTime.Add(10 * time.Minute)
	_, privHit, _ = c.Get(privateUUID)
	_, validHit, _ = c.Get(validUUID)
	if privHit {
		t.Errorf("expected PrivateOrNotFound to expire after 20m")
	}
	if !validHit {
		t.Errorf("expected 200 profile to still be cached at 20m under 6h TTL")
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
