package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/NotRealShanks/badgeboard/internal/roster"
	"github.com/NotRealShanks/badgeboard/internal/tracker"
	tea "github.com/charmbracelet/bubbletea"
)

type progressMsg struct {
	current int
	total   int
	result  model.ProfileResult
}

type doneMsg struct {
	results []model.ProfileResult
	err     error
}

// LiveModel implements tea.Model for live progress display.
type LiveModel struct {
	ctx          context.Context
	cancel       context.CancelFunc
	trk          *tracker.Tracker
	rst          *roster.Roster
	total        int
	current      int
	cacheHits    int
	goal         int
	noColor      bool
	width        int
	startTime    time.Time
	recent       []model.ProfileResult
	finalResults []model.ProfileResult
	err          error
	done         bool
	canceled     bool
}

// NewLiveModel initializes the Bubble Tea model for tracker progress.
func NewLiveModel(ctx context.Context, cancel context.CancelFunc, trk *tracker.Tracker, rst *roster.Roster, goal int, noColor bool) *LiveModel {
	total := 0
	if rst != nil {
		total = len(rst.Students)
	}
	if goal <= 0 {
		goal = 10
	}

	return &LiveModel{
		ctx:       ctx,
		cancel:    cancel,
		trk:       trk,
		rst:       rst,
		total:     total,
		goal:      goal,
		noColor:   noColor,
		width:     80,
		startTime: time.Now(),
		recent:    make([]model.ProfileResult, 0, 5),
	}
}

// Init starts the tracking background worker.
func (m *LiveModel) Init() tea.Cmd {
	return func() tea.Msg {
		return nil
	}
}

// StartTracking runs the tracker and dispatches Bubble Tea messages through program p.
func (m *LiveModel) StartTracking(p *tea.Program) {
	go func() {
		results, err := m.trk.Track(m.ctx, m.rst, func(current, total int, res model.ProfileResult) {
			p.Send(progressMsg{current: current, total: total, result: res})
		})
		p.Send(doneMsg{results: results, err: err})
	}()
}

// Update handles incoming UI events and progress messages.
func (m *LiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.canceled = true
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}

	case progressMsg:
		m.current = msg.current
		if msg.result.FromCache {
			m.cacheHits++
		}
		// Rolling list of last 5 results
		m.recent = append(m.recent, msg.result)
		if len(m.recent) > 5 {
			m.recent = m.recent[len(m.recent)-5:]
		}
		return m, nil

	case doneMsg:
		m.done = true
		m.finalResults = msg.results
		m.err = msg.err
		return m, tea.Quit
	}

	return m, nil
}

// View renders the live terminal view while tracking.
func (m *LiveModel) View() string {
	if m.done {
		return ""
	}

	var b strings.Builder

	elapsed := time.Since(m.startTime)
	rate := 0.0
	if elapsed.Seconds() > 0 && m.current > 0 {
		rate = float64(m.current) / elapsed.Seconds()
	}

	percent := 0.0
	if m.total > 0 {
		percent = float64(m.current) / float64(m.total) * 100.0
	}

	title := "badgeboard tracker | GDG Learn With Communities"
	if !m.noColor {
		title = headerStyle.Render(title)
	}
	b.WriteString(title + "\n\n")

	// Overall Progress Bar
	barWidth := 20
	if m.width <= 80 {
		barWidth = 14
	}
	filled := 0
	if m.total > 0 {
		filled = (m.current * barWidth) / m.total
	}
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled
	barStr := fmt.Sprintf("[%s%s] %d/%d (%.1f%%)",
		strings.Repeat("█", filled), strings.Repeat("░", empty),
		m.current, m.total, percent)
	if !m.noColor {
		barStr = inProgressStyle.Render(barStr)
	}
	b.WriteString(barStr + "\n")

	statsStr := fmt.Sprintf("Rate: %.1f req/s  |  Cache Hits: %d  |  Elapsed: %s",
		rate, m.cacheHits, elapsed.Round(time.Second).String())
	if !m.noColor {
		statsStr = mutedStyle.Render(statsStr)
	}
	b.WriteString(statsStr + "\n\n")

	// Recent activity list
	b.WriteString(headerStyle.Render("Recent Profile Activity:") + "\n")
	if len(m.recent) == 0 {
		b.WriteString(mutedStyle.Render("  Waiting for worker responses...") + "\n")
	} else {
		for _, r := range m.recent {
			name := r.Student.Name
			if name == "" {
				name = r.DisplayName
			}
			if name == "" {
				name = r.Student.ProfileURL
			}
			if len(name) > 24 {
				name = name[:23] + "…"
			}
			status := StatusLabel(r.Status, m.noColor)
			line := fmt.Sprintf("  %-24s  %2d badges  %s", name, r.CountedBadges, status)
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n" + mutedStyle.Render("Press Ctrl+C to stop gracefully and display partial results.") + "\n")

	return b.String()
}

// Results returns the collected final or partial results.
func (m *LiveModel) Results() ([]model.ProfileResult, error) {
	return m.finalResults, m.err
}

// CacheHits returns the number of cache hits recorded during the run.
func (m *LiveModel) CacheHits() int {
	return m.cacheHits
}

// Elapsed returns total runtime duration.
func (m *LiveModel) Elapsed() time.Duration {
	return time.Since(m.startTime)
}

// WasCanceled reports whether the run was halted by user interrupt.
func (m *LiveModel) WasCanceled() bool {
	return m.canceled
}
