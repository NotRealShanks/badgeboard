package tracker

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NotRealShanks/badgeboard/internal/config"
	"github.com/NotRealShanks/badgeboard/internal/fetch"
	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/NotRealShanks/badgeboard/internal/roster"
)


func TestTracker_OrganizerSampleEndToEnd(t *testing.T) {
	allHTML, err := os.ReadFile(filepath.Join("..", "..", "testdata", "real", "all_badges.html"))
	if err != nil {
		t.Fatalf("failed to read all_badges.html: %v", err)
	}
	someHTML, err := os.ReadFile(filepath.Join("..", "..", "testdata", "real", "some_badges.html"))
	if err != nil {
		t.Fatalf("failed to read some_badges.html: %v", err)
	}
	zeroHTML, err := os.ReadFile(filepath.Join("..", "..", "testdata", "real", "zero_badges.html"))
	if err != nil {
		t.Fatalf("failed to read zero_badges.html: %v", err)
	}

	mockClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			switch {
			case strings.Contains(path, "be556f7b-ab4b-4be7-82fb-9be45202ddd7"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(allHTML)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			case strings.Contains(path, "8f0d00c3-7674-4042-ba15-d26a3fae84f9"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(someHTML)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			case strings.Contains(path, "2fafe410-5b6c-463b-9a1d-61dbfb43e9ac"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(zeroHTML)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			case strings.Contains(path, "172773ae-1097-499b-a064-0be32bd06b81"):
				// Real upstream behavior: redirects away from profile to root homepage
				hdr := make(http.Header)
				hdr.Set("Location", "https://www.skills.google/")
				return &http.Response{
					StatusCode: http.StatusFound,
					Header:     hdr,
					Body:       io.NopCloser(bytes.NewReader(nil)),
					Request:    req,
				}, nil
			case path == "/" || path == "":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader([]byte("<html><head><title>Google Skills</title></head><body><h1>Skills</h1></body></html>"))),
					Request:    req,
				}, nil
			default:
				// Random rows: serve valid zero-badge profile
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(zeroHTML)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}
		}),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			lowerPath := strings.ToLower(req.URL.Path)
			if lowerPath == "/" || lowerPath == "" || !strings.Contains(lowerPath, "public_profiles") {
				return fetch.ErrNotFoundOrPrivate
			}
			return nil
		},
	}

	fetcher := fetch.New(
		fetch.WithHTTPClient(mockClient),
	)

	// Load organizer sample CSV directly from testdata
	csvPath := filepath.Join("..", "..", "testdata", "organizer_sample.csv")
	rst, err := roster.LoadFile(csvPath)
	if err != nil {
		t.Fatalf("failed to load organizer sample CSV: %v", err)
	}

	if len(rst.Students) != 6 {
		t.Fatalf("expected 6 students in organizer sample, got %d", len(rst.Students))
	}

	cfg := config.DefaultConfig()
	trk, err := New(cfg, WithFetcher(fetcher))
	if err != nil {
		t.Fatalf("tracker initialization failed: %v", err)
	}

	results, err := trk.Track(context.Background(), rst, nil)
	if err != nil {
		t.Fatalf("tracking failed: %v", err)
	}

	if len(results) != 6 {
		t.Fatalf("expected 6 results, got %d", len(results))
	}

	// Row 1: All skill badges completed -> Complete
	if results[0].Status != model.StatusComplete {
		t.Errorf("Row 1 expected StatusComplete, got: %s", results[0].Status)
	}
	if results[0].CountedBadges != 10 {
		t.Errorf("Row 1 expected 10 badges, got: %d", results[0].CountedBadges)
	}

	// Row 2: Some skill badges completed -> InProgress
	if results[1].Status != model.StatusInProgress {
		t.Errorf("Row 2 expected StatusInProgress, got: %s (err: %s)", results[1].Status, results[1].Error)
	}
	if results[1].CountedBadges < 1 || results[1].CountedBadges >= 10 {
		t.Errorf("Row 2 expected 1..9 badges, got: %d", results[1].CountedBadges)
	}

	// Row 3: None of the skill badges completed -> NoBadges
	if results[2].Status != model.StatusNoBadges {
		t.Errorf("Row 3 expected StatusNoBadges, got: %s", results[2].Status)
	}
	if results[2].CountedBadges != 0 {
		t.Errorf("Row 3 expected 0 badges, got: %d", results[2].CountedBadges)
	}

	// Row 4: Account which is now private -> PrivateOrNotFound
	if results[3].Status != model.StatusPrivateOrNotFound {
		t.Errorf("Row 4 expected StatusPrivateOrNotFound, got: %s", results[3].Status)
	}

	// Rows 5 & 6: Random rows -> valid and non-crashing
	for i := 4; i <= 5; i++ {
		st := results[i].Status
		if st != model.StatusComplete && st != model.StatusInProgress && st != model.StatusNoBadges && st != model.StatusPrivateOrNotFound {
			t.Errorf("Row %d expected valid non-crashing status, got: %s", i+1, st)
		}
	}
}
