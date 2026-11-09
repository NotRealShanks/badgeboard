package ui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/charmbracelet/lipgloss"
)

var (
	ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252"))

	completeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("42")) // Green

	inProgressStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")) // Blue/Cyan

	noBadgesStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")) // Gray

	unavailableStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("208")) // Orange/Yellow

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")) // Red

	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238"))
)

// StripANSI removes ANSI escape codes for golden file testing and non-TTY mode.
func StripANSI(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}

// RenderProgressBar generates a 10-segment visual progress bar scaled to goal.
func RenderProgressBar(counted, goal int, noColor bool) string {
	if goal <= 0 {
		goal = 10
	}
	segments := 10
	filled := (counted * segments) / goal
	if filled > segments {
		filled = segments
	}
	if filled < 0 {
		filled = 0
	}
	empty := segments - filled

	filledBar := strings.Repeat("█", filled)
	emptyBar := strings.Repeat("░", empty)

	if noColor {
		return "[" + filledBar + emptyBar + "]"
	}

	if filled == segments {
		return completeStyle.Render("[" + filledBar + emptyBar + "]")
	}
	if filled > 0 {
		return inProgressStyle.Render("[" + filledBar) + mutedStyle.Render(emptyBar + "]")
	}
	return mutedStyle.Render("[" + emptyBar + "]")
}

// StatusLabel returns an accessible label with symbol and optional color.
func StatusLabel(s model.Status, noColor bool) string {
	var symbol, text string
	var style lipgloss.Style

	switch s {
	case model.StatusComplete:
		symbol = "[✓]"
		text = "Complete"
		style = completeStyle
	case model.StatusInProgress:
		symbol = "[>]"
		text = "In Progress"
		style = inProgressStyle
	case model.StatusNoBadges:
		symbol = "[-]"
		text = "No Badges"
		style = noBadgesStyle
	case model.StatusPrivateOrNotFound:
		symbol = "[?]"
		text = "Private/404"
		style = unavailableStyle
	case model.StatusRateLimited:
		symbol = "[!]"
		text = "Rate Limited"
		style = errorStyle
	case model.StatusNetworkError:
		symbol = "[!]"
		text = "Network Err"
		style = errorStyle
	case model.StatusParseError:
		symbol = "[!]"
		text = "Parse Err"
		style = errorStyle
	case model.StatusInvalidURL:
		symbol = "[!]"
		text = "Invalid URL"
		style = errorStyle
	default:
		symbol = "[?]"
		text = string(s)
		style = mutedStyle
	}

	combined := fmt.Sprintf("%-3s %s", symbol, text)
	if noColor {
		return combined
	}
	return style.Render(combined)
}

// TableOptions configures rendering of the terminal table.
type TableOptions struct {
	Goal       int
	NoColor    bool
	Width      int // Terminal width; wraps/truncates for <= 80 columns
	CacheHits  int
	BadRows    int
	Duration   time.Duration
}

// RenderFinalTable formats all results into evaluated and unavailable sections with footer metrics.
func RenderFinalTable(results []model.ProfileResult, opts TableOptions) string {
	if opts.Goal <= 0 {
		opts.Goal = 10
	}
	if opts.Width <= 0 {
		opts.Width = 100
	}

	var evaluated []model.ProfileResult
	var unavailable []model.ProfileResult

	for _, r := range results {
		switch r.Status {
		case model.StatusComplete, model.StatusInProgress, model.StatusNoBadges:
			evaluated = append(evaluated, r)
		default:
			unavailable = append(unavailable, r)
		}
	}

	// Sort evaluated: CountedBadges DESC, then Name ASC
	sort.Slice(evaluated, func(i, j int) bool {
		if evaluated[i].CountedBadges != evaluated[j].CountedBadges {
			return evaluated[i].CountedBadges > evaluated[j].CountedBadges
		}
		nameI := strings.ToLower(evaluated[i].Student.Name)
		if nameI == "" {
			nameI = strings.ToLower(evaluated[i].DisplayName)
		}
		nameJ := strings.ToLower(evaluated[j].Student.Name)
		if nameJ == "" {
			nameJ = strings.ToLower(evaluated[j].DisplayName)
		}
		return nameI < nameJ
	})

	// Sort unavailable: Status ASC, then Name ASC
	sort.Slice(unavailable, func(i, j int) bool {
		if unavailable[i].Status != unavailable[j].Status {
			return unavailable[i].Status < unavailable[j].Status
		}
		return strings.ToLower(unavailable[i].Student.Name) < strings.ToLower(unavailable[j].Student.Name)
	})

	var b strings.Builder

	// Header
	nameWidth := 24
	if opts.Width <= 80 {
		nameWidth = 18
	}

	header := fmt.Sprintf("%-*s  %-12s  %5s  %-14s  %-10s",
		nameWidth, "STUDENT", "PROGRESS", "COUNT", "STATUS", "LAST EARNED")
	if !opts.NoColor {
		header = headerStyle.Render(header)
	}
	b.WriteString(header + "\n")

	sepLen := opts.Width
	if sepLen > 90 {
		sepLen = 90
	}
	separator := strings.Repeat("─", sepLen)
	if !opts.NoColor {
		separator = separatorStyle.Render(separator)
	}
	b.WriteString(separator + "\n")

	if len(evaluated) == 0 && len(unavailable) == 0 {
		b.WriteString(mutedStyle.Render("No participants loaded from roster.\n"))
	}

	// Evaluated Section
	for _, r := range evaluated {
		name := r.Student.Name
		if name == "" {
			name = r.DisplayName
		}
		if name == "" {
			name = "Anonymous"
		}
		if len(name) > nameWidth {
			name = name[:nameWidth-1] + "…"
		}

		progressBar := RenderProgressBar(r.CountedBadges, opts.Goal, opts.NoColor)
		countStr := fmt.Sprintf("%2d/%-2d", r.CountedBadges, opts.Goal)
		statusStr := StatusLabel(r.Status, opts.NoColor)

		lastDate := "-"
		if r.LastBadgeEarnedAt != nil && !r.LastBadgeEarnedAt.IsZero() {
			lastDate = r.LastBadgeEarnedAt.Format("2006-01-02")
		}

		row := fmt.Sprintf("%-*s  %-12s  %5s  %-14s  %-10s",
			nameWidth, name, progressBar, countStr, statusStr, lastDate)
		b.WriteString(row + "\n")
	}

	// Unavailable Section (Separated)
	if len(unavailable) > 0 {
		b.WriteString("\n")
		unavailHeader := "UNAVAILABLE / UNREACHABLE PROFILES"
		if !opts.NoColor {
			unavailHeader = unavailableStyle.Render(unavailHeader)
		}
		b.WriteString(unavailHeader + "\n")
		b.WriteString(separator + "\n")

		for _, r := range unavailable {
			name := r.Student.Name
			if name == "" {
				name = r.Student.ProfileURL
			}
			if len(name) > nameWidth {
				name = name[:nameWidth-1] + "…"
			}

			statusStr := StatusLabel(r.Status, opts.NoColor)
			detail := r.Error
			if detail == "" {
				detail = "profile hidden or not found"
			}
			maxDetail := opts.Width - nameWidth - 20
			if maxDetail > 45 {
				maxDetail = 45
			}
			if maxDetail > 10 && len(detail) > maxDetail {
				detail = detail[:maxDetail-1] + "…"
			}

			row := fmt.Sprintf("%-*s  %-14s  %s", nameWidth, name, statusStr, detail)
			b.WriteString(row + "\n")
		}
	}

	// Footer Metrics
	b.WriteString(separator + "\n")

	total := len(results)
	completeCount := 0
	inProgressCount := 0
	noBadgesCount := 0
	unavailCount := len(unavailable)

	for _, r := range results {
		switch r.Status {
		case model.StatusComplete:
			completeCount++
		case model.StatusInProgress:
			inProgressCount++
		case model.StatusNoBadges:
			noBadgesCount++
		}
	}

	completionRate := 0.0
	if total > 0 {
		completionRate = float64(completeCount) / float64(total) * 100.0
	}

	footerLine1 := fmt.Sprintf("Total: %d  |  Complete: %d (%.1f%%)  |  In Progress: %d  |  No Badges: %d  |  Unavailable: %d",
		total, completeCount, completionRate, inProgressCount, noBadgesCount, unavailCount)

	durStr := opts.Duration.Round(time.Millisecond).String()
	if opts.Duration == 0 {
		durStr = "<1s"
	}
	footerLine2 := fmt.Sprintf("Cache Hits: %d  |  Bad CSV Rows: %d  |  Duration: %s",
		opts.CacheHits, opts.BadRows, durStr)

	if !opts.NoColor {
		footerLine1 = headerStyle.Render(footerLine1)
		footerLine2 = mutedStyle.Render(footerLine2)
	}

	b.WriteString(footerLine1 + "\n")
	b.WriteString(footerLine2 + "\n")

	return b.String()
}
