package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/NotRealShanks/badgeboard/internal/model"
)

func TestTUI_CtrlCStopsGracefully(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewLiveModel(ctx, cancel, nil, nil, 10, 1.0, true)

	// Send Ctrl+C key event
	updatedModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	lm, ok := updatedModel.(*LiveModel)
	if !ok {
		t.Fatalf("expected *LiveModel, got %T", updatedModel)
	}

	if !lm.canceled {
		t.Errorf("expected lm.canceled=true after Ctrl+C")
	}
	if ctx.Err() == nil {
		t.Errorf("expected context to be cancelled after Ctrl+C")
	}
	if cmd == nil {
		t.Errorf("expected quit command after Ctrl+C")
	}
}

func TestTUI_ProgressAndRecentActivity(t *testing.T) {
	ctx := context.Background()
	m := NewLiveModel(ctx, nil, nil, nil, 10, 1.0, true)
	m.total = 5

	// Send progress message with cache hit
	res1 := model.ProfileResult{
		Student: model.Student{
			UUID: "be556f7b-ab4b-4be7-82fb-9be45202ddd7",
		},
		Status:        model.StatusComplete,
		CountedBadges: 10,
		FromCache:     true,
	}

	updated, _ := m.Update(progressMsg{current: 1, total: 5, result: res1})
	lm := updated.(*LiveModel)

	if lm.current != 1 {
		t.Errorf("expected current=1, got %d", lm.current)
	}
	if lm.cacheHits != 1 {
		t.Errorf("expected cacheHits=1, got %d", lm.cacheHits)
	}

	view := lm.View()
	if !strings.Contains(view, "Cached: 1") {
		t.Errorf("expected View to display 'Cached: 1', got:\n%s", view)
	}
	if !strings.Contains(view, "Profile (be55…)") {
		t.Errorf("expected View to use fallback UUID label, got:\n%s", view)
	}
}

func TestTUI_DoneMsgExits(t *testing.T) {
	m := NewLiveModel(context.Background(), nil, nil, nil, 10, 1.0, true)
	results := []model.ProfileResult{
		{Sample Learner},
	}

	updated, cmd := m.Update(doneMsg{results: results, err: nil})
	lm := updated.(*LiveModel)

	if !lm.done {
		t.Errorf("expected done=true")
	}
	if cmd == nil {
		t.Errorf("expected tea.Quit command")
	}

	res, err := lm.Results()
	if err != nil || len(res) != 1 {
		t.Fatalf("unexpected Results: %v, %v", res, err)
	}
}
