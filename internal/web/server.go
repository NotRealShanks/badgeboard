package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/config"
	"github.com/NotRealShanks/badgeboard/internal/export"
	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/NotRealShanks/badgeboard/internal/roster"
	"github.com/NotRealShanks/badgeboard/internal/tracker"
)

// ServerConfig holds server options and security boundaries.
type ServerConfig struct {
	Addr           string
	Hosted         bool
	MaxRows        int
	MaxRuns        int
	RunTTL         time.Duration
	MaxRPS         float64
	MaxConcurrency int
}

// Server is the HTTP server for badgeboard serve.
type Server struct {
	cfg     ServerConfig
	mux     *http.ServeMux
	store   *Store
	runner  *Runner
	tracker *tracker.Tracker

	// Per-IP rate limiter for hosted mode
	ipMu      sync.Mutex
	ipLimits  map[string]*ipRateLimiter
}

type ipRateLimiter struct {
	tokens     float64
	lastRefill time.Time
}

// NewServer constructs the badgeboard HTTP server.
func NewServer(cfg ServerConfig, trk *tracker.Tracker) *Server {
	if cfg.MaxRows <= 0 {
		cfg.MaxRows = 500
	}
	if cfg.MaxRuns <= 0 {
		cfg.MaxRuns = 3
	}
	if cfg.RunTTL <= 0 {
		cfg.RunTTL = 30 * time.Minute
	}
	if cfg.MaxRPS <= 0 {
		cfg.MaxRPS = 2.0
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 4
	}

	store := NewStore(cfg.MaxRuns, cfg.RunTTL)
	runner := NewRunner(trk, store)

	s := &Server{
		cfg:      cfg,
		mux:      http.NewServeMux(),
		store:    store,
		runner:   runner,
		tracker:  trk,
		ipLimits: make(map[string]*ipRateLimiter),
	}

	s.routes()
	return s
}

// Handler returns the root HTTP handler wrapped with security headers.
func (s *Server) Handler() http.Handler {
	return s.securityMiddleware(s.mux)
}

func (s *Server) routes() {
	// Health check
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)

	// API Endpoints
	s.mux.HandleFunc("GET /api/demo", s.handleDemo)
	s.mux.HandleFunc("POST /api/runs", s.handleCreateRun)
	s.mux.HandleFunc("GET /api/runs/{id}/events", s.handleRunEvents)
	s.mux.HandleFunc("GET /api/runs/{id}", s.handleGetRun)
	s.mux.HandleFunc("POST /api/runs/{id}/cancel", s.handleCancelRun)
	s.mux.HandleFunc("POST /api/runs/{id}/retry", s.handleRetryRun)
	s.mux.HandleFunc("GET /api/runs/{id}/export.csv", s.handleExportCSV)
	s.mux.HandleFunc("GET /api/runs/{id}/export.json", s.handleExportJSON)
	s.mux.HandleFunc("GET /api/runs/{id}/report.html", s.handleExportReportHTML)

	// Embedded Static Frontend
	assetFS := http.FileServer(http.FS(Assets()))
	s.mux.Handle("GET /", assetFS)
}

// securityMiddleware sets strict CSP, nosniff, and origin checks on all requests.
func (s *Server) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strict CSP
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self';")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")

		// Prevent caching on API endpoints
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
			w.Header().Set("Pragma", "no-cache")
		}

		// Origin check on state-changing POST requests
		if r.Method == http.MethodPost {
			origin := r.Header.Get("Origin")
			if origin != "" {
				host := r.Host
				if !strings.HasSuffix(origin, host) && !strings.Contains(origin, "127.0.0.1") && !strings.Contains(origin, "localhost") {
					http.Error(w, "Forbidden: Cross-origin POST request denied", http.StatusForbidden)
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleDemo(w http.ResponseWriter, r *http.Request) {
	// Returns deterministic demo data
	w.Header().Set("Content-Type", "application/json")
	demoSnapshot := map[string]any{
		"status": "completed",
		"summary": map[string]any{
			"goal":                     10,
			"total_students":          15,
			"complete_count":          4,
			"in_progress_count":       5,
			"no_badges_count":         2,
			"unreachable_count":       4,
			"network_requests":        12,
			"cache_hits":              8,
			"retries":                 1,
			"rate_limit_pause_seconds": 1.8,
			"duration_seconds":        9.2,
			"bad_rows_count":          0,
			"anonymized":               false,
		},
		"results": []map[string]any{
			{"display_name": "Corin Calder", "counted_badges": 10, "goal": 10, "status": "complete", "last_badge_date": "2026-11-05"},
			{"display_name": "Lyra Vance", "counted_badges": 10, "goal": 10, "status": "complete", "last_badge_date": "2026-11-04"},
			{"display_name": "Orion Sterling", "counted_badges": 10, "goal": 10, "status": "complete", "last_badge_date": "2026-11-03"},
			{"display_name": "Zephyr Drake", "counted_badges": 10, "goal": 10, "status": "complete", "last_badge_date": "2026-11-03"},
			{"display_name": "Astra Thorne", "counted_badges": 8, "goal": 10, "status": "in_progress", "last_badge_date": "2026-10-31"},
			{"display_name": "Talon Mercer", "counted_badges": 6, "goal": 10, "status": "in_progress", "last_badge_date": "2026-10-28"},
			{"display_name": "Vesper Rowe", "counted_badges": 5, "goal": 10, "status": "in_progress", "last_badge_date": "2026-10-24"},
			{"display_name": "Cassian Vane", "counted_badges": 3, "goal": 10, "status": "in_progress", "last_badge_date": "2026-10-21"},
			{"display_name": "Sonya Vale", "counted_badges": 1, "goal": 10, "status": "in_progress", "last_badge_date": "2026-10-18"},
			{"display_name": "Jaxen Cross", "counted_badges": 0, "goal": 10, "status": "no_badges", "last_badge_date": ""},
			{"display_name": "Rylan Solis", "counted_badges": 0, "goal": 10, "status": "no_badges", "last_badge_date": ""},
		},
		"unreachable": []map[string]any{
			{"display_name": "Brecken Hale", "status": "network_error", "reason": "Network timeout dialing skills.google"},
			{"display_name": "Kaelen Frost", "status": "private_or_not_found", "reason": "Profile set to private or returned 404"},
			{"display_name": "Thalia Voss", "status": "private_or_not_found", "reason": "Profile set to private or returned 404"},
			{"display_name": "Silas Novak", "status": "rate_limited", "reason": "HTTP 429: rate limit exceeded"},
		},
	}
	_ = json.NewEncoder(w).Encode(demoSnapshot)
}

type createRunRequest struct {
	URLs          []string `json:"urls"`
	SheetURL      string   `json:"sheet_url"`
	Goal          int      `json:"goal"`
	RPS           float64  `json:"rps"`
	Concurrency   int      `json:"concurrency"`
	Anonymize     bool     `json:"anonymize"`
	IncludeBadges bool     `json:"include_badges"`
	BypassCache   bool     `json:"bypass_cache"`
}

func (s *Server) checkIPRateLimit(ip string) bool {
	if !s.cfg.Hosted {
		return true
	}

	s.ipMu.Lock()
	defer s.ipMu.Unlock()

	now := time.Now()
	lim, ok := s.ipLimits[ip]
	if !ok {
		lim = &ipRateLimiter{tokens: 5.0, lastRefill: now}
		s.ipLimits[ip] = lim
	}

	// Refill at 5 tokens per minute (1 token every 12s)
	elapsed := now.Sub(lim.lastRefill).Seconds()
	lim.tokens += elapsed * (5.0 / 60.0)
	if lim.tokens > 5.0 {
		lim.tokens = 5.0
	}
	lim.lastRefill = now

	if lim.tokens >= 1.0 {
		lim.tokens -= 1.0
		return true
	}
	return false
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	// Enforce 1 MB request body limit
	const maxUploadSize = 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	// IP Rate limit in hosted mode
	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}
	if !s.checkIPRateLimit(clientIP) {
		http.Error(w, `{"error":"Too many run requests. Please wait a minute before starting another run."}`, http.StatusTooManyRequests)
		return
	}

	var req createRunRequest
	var rawCSV []byte

	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/form-data") {
		err := r.ParseMultipartForm(maxUploadSize)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"Parsing upload: %v"}`, err), http.StatusBadRequest)
			return
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, `{"error":"No file provided in form-data"}`, http.StatusBadRequest)
			return
		}
		defer file.Close()

		rawCSV, err = io.ReadAll(file)
		if err != nil {
			http.Error(w, `{"error":"Reading uploaded file failed"}`, http.StatusBadRequest)
			return
		}

		req.Goal, _ = strconv.Atoi(r.FormValue("goal"))
		req.RPS, _ = strconv.ParseFloat(r.FormValue("rps"), 64)
		req.Concurrency, _ = strconv.Atoi(r.FormValue("concurrency"))
		req.Anonymize, _ = strconv.ParseBool(r.FormValue("anonymize"))
		req.IncludeBadges, _ = strconv.ParseBool(r.FormValue("include_badges"))
		req.BypassCache, _ = strconv.ParseBool(r.FormValue("bypass_cache"))
	} else if strings.HasPrefix(contentType, "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"Invalid JSON request body: %v"}`, err), http.StatusBadRequest)
			return
		}
	} else {
		http.Error(w, `{"error":"Unsupported Content-Type (must be multipart/form-data or application/json)"}`, http.StatusBadRequest)
		return
	}

	// Apply defaults and server caps
	if req.Goal <= 0 {
		req.Goal = 10
	}
	if req.RPS <= 0 || req.RPS > s.cfg.MaxRPS {
		req.RPS = s.cfg.MaxRPS
	}
	if req.Concurrency <= 0 || req.Concurrency > s.cfg.MaxConcurrency {
		req.Concurrency = s.cfg.MaxConcurrency
	}

	var rst *roster.Roster

	// Input Option 1: File Upload
	if len(rawCSV) > 0 {
		var err error
		rst, err = roster.Load(bytes.NewReader(rawCSV))
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"Parsing CSV roster: %v"}`, err), http.StatusBadRequest)
			return
		}
	} else if req.SheetURL != "" {
		// Input Option 2: Google Sheet published CSV link (SSRF protected)
		csvBytes, err := FetchPublishedSheetCSV(r.Context(), req.SheetURL)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"Fetching published Google Sheet: %v"}`, err), http.StatusBadRequest)
			return
		}
		rst, err = roster.Load(bytes.NewReader(csvBytes))
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"Parsing Sheet CSV: %v"}`, err), http.StatusBadRequest)
			return
		}
	} else if len(req.URLs) > 0 {
		// Input Option 3: Pasted URLs list
		var students []model.Student
		var badRows []roster.BadRowReport
		for idx, u := range req.URLs {
			u = strings.TrimSpace(u)
			if u == "" {
				continue
			}
			canonical, uuid, err := roster.NormalizeURL(u)
			if err != nil {
				badRows = append(badRows, roster.BadRowReport{
					RowNumber: idx + 1,
					RawValue:  u,
					Reason:    err.Error(),
				})
				continue
			}
			students = append(students, model.Student{
				ProfileURL: canonical,
				UUID:       uuid,
				RowNumber:  idx + 1,
			})
		}
		rst = &roster.Roster{
			Students: students,
			BadRows:  badRows,
		}
	} else {
		http.Error(w, `{"error":"No input provided (provide a CSV file, URLs list, or published Sheet link)"}`, http.StatusBadRequest)
		return
	}

	// Validate max rows
	if len(rst.Students) == 0 {
		http.Error(w, `{"error":"Roster contains zero valid student profile URLs."}`, http.StatusBadRequest)
		return
	}
	if len(rst.Students) > s.cfg.MaxRows {
		http.Error(w, fmt.Sprintf(`{"error":"Roster exceeds server limit of %d rows (got %d rows)"}`, s.cfg.MaxRows, len(rst.Students)), http.StatusBadRequest)
		return
	}

	settings := RunSettings{
		Goal:          req.Goal,
		RPS:           req.RPS,
		Concurrency:   req.Concurrency,
		Anonymize:     req.Anonymize,
		IncludeBadges: req.IncludeBadges,
		BypassCache:   req.BypassCache,
	}

	run, err := s.store.CreateRun(settings, len(rst.Students))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusServiceUnavailable)
		return
	}

	// Start run execution in background
	go s.runner.ExecuteRun(context.Background(), run, rst)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"run_id": run.ID,
		"status": run.Status,
		"total":  len(rst.Students),
	})
}

func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, ok := s.store.Get(runID)
	if !ok {
		http.Error(w, "Run not found or expired", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// SSE Headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	lastEventID := 0
	if idStr := r.Header.Get("Last-Event-ID"); idStr != "" {
		lastEventID, _ = strconv.Atoi(idStr)
	} else if idStr = r.URL.Query().Get("last_event_id"); idStr != "" {
		lastEventID, _ = strconv.Atoi(idStr)
	}

	eventCh, unsubscribe := run.Subscribe(lastEventID)
	defer unsubscribe()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// Initial heartbeat flush
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case e, ok := <-eventCh:
			if !ok {
				return
			}
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.Event, e.Data)
			flusher.Flush()

			if e.Event == "run_complete" || e.Event == "run_cancelled" || e.Event == "error" {
				return
			}
		}
	}
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, ok := s.store.Get(runID)
	if !ok {
		http.Error(w, "Run not found or expired", http.StatusNotFound)
		return
	}

	run.mu.RLock()
	defer run.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(run)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, ok := s.store.Get(runID)
	if !ok {
		http.Error(w, "Run not found or expired", http.StatusNotFound)
		return
	}

	cancelled := run.Cancel()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"run_id":    run.ID,
		"cancelled": cancelled,
		"status":    run.Status,
	})
}

func (s *Server) handleRetryRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, ok := s.store.Get(runID)
	if !ok {
		http.Error(w, "Run not found or expired", http.StatusNotFound)
		return
	}

	err := s.runner.RetryUnreachable(context.Background(), run)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"run_id": run.ID,
		"status": "retrying",
	})
}

func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, ok := s.store.Get(runID)
	if !ok {
		http.Error(w, "Run not found or expired", http.StatusNotFound)
		return
	}

	run.mu.RLock()
	results := run.Results
	goal := run.Settings.Goal
	anonymize := run.Settings.Anonymize
	salt := run.Salt
	run.mu.RUnlock()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="badgeboard_%s.csv"`, runID[:8]))

	_ = export.CSV(w, results, goal, anonymize, salt)
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, ok := s.store.Get(runID)
	if !ok {
		http.Error(w, "Run not found or expired", http.StatusNotFound)
		return
	}

	run.mu.RLock()
	defer run.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="badgeboard_%s.json"`, runID[:8]))
	_ = json.NewEncoder(w).Encode(run)
}

func (s *Server) handleExportReportHTML(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, ok := s.store.Get(runID)
	if !ok {
		http.Error(w, "Run not found or expired", http.StatusNotFound)
		return
	}

	run.mu.RLock()
	defer run.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="badgeboard_%s_report.html"`, runID[:8]))

	// Simple embedded HTML report generator matching report.js
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>Badgeboard Report - %s</title></head><body>`, runID[:8])
	fmt.Fprintf(w, `<h1>Badgeboard Report</h1><p>Run ID: %s</p><p>Total: %d, Completed: %d</p></body></html>`, runID, run.Summary.TotalStudents, run.Summary.CompleteCount)
}

// BuildServerTracker initializes the shared engine Tracker according to server flags.
func BuildServerTracker(cfg ServerConfig) (*tracker.Tracker, error) {
	appCfg := config.DefaultConfig()
	appCfg.RateLimit.RPS = cfg.MaxRPS
	appCfg.RateLimit.Concurrency = cfg.MaxConcurrency

	if cfg.Hosted {
		// Hosted mode uses in-memory / no disk cache
		return tracker.New(appCfg, tracker.WithNoCache())
	}

	// Local mode uses standard disk cache
	return tracker.New(appCfg)
}
