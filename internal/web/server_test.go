package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/config"
	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/NotRealShanks/badgeboard/internal/tracker"
)

func setupTestServer(t *testing.T, hosted bool, maxRows, maxRuns int) *Server {
	t.Helper()
	cfg := ServerConfig{
		Addr:           "127.0.0.1:0",
		Hosted:         hosted,
		MaxRows:        maxRows,
		MaxRuns:        maxRuns,
		RunTTL:         1 * time.Second, // short for testing
		MaxRPS:         10.0,
		MaxConcurrency: 4,
	}

	appCfg := config.DefaultConfig()
	trk, err := tracker.New(appCfg, tracker.WithNoCache())
	if err != nil {
		t.Fatalf("failed to build test tracker: %v", err)
	}

	return NewServer(cfg, trk)
}

func TestServer_Healthz(t *testing.T) {
	s := setupTestServer(t, false, 50, 3)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var res map[string]any
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decoding healthz response: %v", err)
	}
	if res["status"] != "ok" {
		t.Errorf("expected status ok, got %v", res["status"])
	}
}

func TestServer_SecurityHeaders(t *testing.T) {
	s := setupTestServer(t, false, 50, 3)

	req := httptest.NewRequest(http.MethodGet, "/api/demo", nil)
	w := httptest.NewRecorder()

	s.Handler().ServeHTTP(w, req)

	headers := w.Header()
	if !strings.Contains(headers.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Errorf("missing strict CSP: got %q", headers.Get("Content-Security-Policy"))
	}
	if headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("missing X-Content-Type-Options nosniff: got %q", headers.Get("X-Content-Type-Options"))
	}
	if headers.Get("X-Frame-Options") != "DENY" {
		t.Errorf("missing X-Frame-Options DENY: got %q", headers.Get("X-Frame-Options"))
	}
	if headers.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("missing Referrer-Policy no-referrer: got %q", headers.Get("Referrer-Policy"))
	}
	if !strings.Contains(headers.Get("Cache-Control"), "no-store") {
		t.Errorf("missing Cache-Control no-store on API: got %q", headers.Get("Cache-Control"))
	}
}

func TestServer_OriginCheck(t *testing.T) {
	s := setupTestServer(t, false, 50, 3)

	// Cross-origin malicious POST
	req := httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://malicious-website.com")
	req.Host = "badgeboard.local:8080"
	w := httptest.NewRecorder()

	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden on cross-origin POST, got %d", w.Code)
	}
}

func TestServer_Limits_MaxRows(t *testing.T) {
	s := setupTestServer(t, false, 3, 3) // max 3 rows

	urls := []string{
		"https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555551",
		"https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555552",
		"https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555553",
		"https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555554", // 4th row exceeds limit 3
	}

	body, _ := json.Marshal(map[string]any{"urls": urls})
	req := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request when exceeding maxRows, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestServer_Limits_MaxRuns(t *testing.T) {
	s := setupTestServer(t, false, 10, 1) // max 1 active run

	// Manually create an active run in store
	_, err := s.store.CreateRun(RunSettings{Goal: 10}, 5)
	if err != nil {
		t.Fatalf("initial run creation failed: %v", err)
	}

	// 2nd run creation should fail due to limit
	_, err2 := s.store.CreateRun(RunSettings{Goal: 10}, 5)
	if err2 == nil {
		t.Errorf("expected error when exceeding maxRuns, got nil")
	}
}

func TestServer_Store_TTLJanitor(t *testing.T) {
	store := NewStore(5, 50*time.Millisecond) // 50ms TTL

	run, err := store.CreateRun(RunSettings{Goal: 10}, 2)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Immediate lookup succeeds
	if _, ok := store.Get(run.ID); !ok {
		t.Errorf("expected to find run %s immediately", run.ID)
	}

	// Wait past TTL
	time.Sleep(70 * time.Millisecond)

	// Lookup fails after expiration
	if _, ok := store.Get(run.ID); ok {
		t.Errorf("expected run %s to be expired after TTL", run.ID)
	}

	// Force purge
	store.purgeExpired()
	store.mu.RLock()
	count := len(store.runs)
	store.mu.RUnlock()
	if count != 0 {
		t.Errorf("expected 0 runs after purge, got %d", count)
	}
}

func TestServer_SSE_StreamAndCancel(t *testing.T) {
	s := setupTestServer(t, false, 10, 3)

	run, err := s.store.CreateRun(RunSettings{Goal: 10}, 2)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Broadcast sample events
	go func() {
		time.Sleep(10 * time.Millisecond)
		run.Broadcast("progress", `{"done":1,"total":2}`)
		time.Sleep(10 * time.Millisecond)
		run.Cancel()
	}()

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/runs/%s/events", run.ID), nil).WithContext(ctx)
	w := httptest.NewRecorder()

	s.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "event: progress") {
		t.Errorf("SSE stream missing progress event: %s", body)
	}
	if !strings.Contains(body, "event: run_cancelled") {
		t.Errorf("SSE stream missing run_cancelled event: %s", body)
	}
}

func TestServer_Anonymization_Audit(t *testing.T) {
	s := setupTestServer(t, false, 10, 3)

	run, err := s.store.CreateRun(RunSettings{
		Goal:          10,
		Anonymize:     true,
		IncludeBadges: false, // Strict public tier
	}, 1)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	realUUID := "11111111-2222-3333-4444-555555555555"
	realName := "Secret Learner"
	realURL := "https://www.skills.google/public_profiles/" + realUUID

	rawResult := model.ProfileResult{
		Student: model.Student{
			Name:       realName,
			ProfileURL: realURL,
			UUID:       realUUID,
		},
		DisplayName:   realName,
		Status:        model.StatusComplete,
		CountedBadges: 10,
		TotalBadges:   10,
		League:        "Gold League",
		Points:        500,
		Badges: []model.BadgeRecord{
			{Title: "Secret Badge Title", EarnedAt: time.Now()},
		},
	}

	sanitized := run.SanitizeResult(rawResult)

	// Audit assertions:
	if sanitized.DisplayName == realName {
		t.Errorf("real name leaked in DisplayName: %s", sanitized.DisplayName)
	}
	if !strings.HasPrefix(sanitized.DisplayName, "Learner-") {
		t.Errorf("expected pseudonym format Learner-XXXX, got %s", sanitized.DisplayName)
	}
	if sanitized.Student.UUID != "" {
		t.Errorf("UUID leaked in student struct: %s", sanitized.Student.UUID)
	}
	if sanitized.Student.ProfileURL != "" {
		t.Errorf("ProfileURL leaked in student struct: %s", sanitized.Student.ProfileURL)
	}
	if sanitized.League != "" {
		t.Errorf("League leaked in public tier: %s", sanitized.League)
	}
	if sanitized.Points != 0 {
		t.Errorf("Points leaked in public tier: %d", sanitized.Points)
	}
	if len(sanitized.Badges) != 0 {
		t.Errorf("Badge details leaked in strict public tier: %v", sanitized.Badges)
	}

	// Check JSON serialization
	jsonBytes, _ := json.Marshal(sanitized)
	jsonStr := string(jsonBytes)

	if strings.Contains(jsonStr, realUUID) {
		t.Errorf("raw UUID found in JSON: %s", jsonStr)
	}
	if strings.Contains(jsonStr, realName) {
		t.Errorf("real name found in JSON: %s", jsonStr)
	}
	if strings.Contains(jsonStr, "Secret Badge Title") {
		t.Errorf("badge title found in strict public JSON: %s", jsonStr)
	}
}

func TestServer_MultipartCSVUpload(t *testing.T) {
	s := setupTestServer(t, false, 50, 3)

	csvData := `Google Skills Profile URL
https://www.skills.google/public_profiles/11111111-2222-3333-4444-555555555555
https://www.skills.google/public_profiles/22222222-3333-4444-5555-666666666666
`

	bodyBuf := &bytes.Buffer{}
	mpWriter := multipart.NewWriter(bodyBuf)
	filePart, err := mpWriter.CreateFormFile("file", "roster.csv")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = filePart.Write([]byte(csvData))
	_ = mpWriter.WriteField("goal", "10")
	_ = mpWriter.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/runs", bodyBuf)
	req.Header.Set("Content-Type", mpWriter.FormDataContentType())
	w := httptest.NewRecorder()

	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d (body: %s)", w.Code, w.Body.String())
	}

	var res map[string]any
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if res["run_id"] == nil || res["run_id"] == "" {
		t.Errorf("expected non-empty run_id, got %v", res["run_id"])
	}
	if res["total"] != float64(2) {
		t.Errorf("expected total 2, got %v", res["total"])
	}
}

func TestServer_SharedLimiter_Concurrency(t *testing.T) {
	// Proves that multiple runs share the server tracker and execute concurrently safely
	s := setupTestServer(t, false, 50, 5)

	var wg sync.WaitGroup
	errs := make(chan error, 4)

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			run, err := s.store.CreateRun(RunSettings{Goal: 10}, 2)
			if err != nil {
				errs <- err
				return
			}
			if run.ID == "" {
				errs <- fmt.Errorf("empty run ID")
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent run creation error: %v", err)
	}
}
