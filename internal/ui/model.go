// Package ui implements the Bubble Tea terminal UI: three colored panels
// (top stars, weekly trending, monthly trending) fed by a background
// scanner, plus a status/help footer.
package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hrmeetsingh/github-most-stars/internal/github"
	"github.com/hrmeetsingh/github-most-stars/internal/scanner"
)

const listSize = 10

var (
	colorTitle    = lipgloss.Color("#F5F5F5")
	colorTopStars = lipgloss.Color("#7AA2F7") // blue
	colorWeekly   = lipgloss.Color("#9ECE6A") // green
	colorMonthly  = lipgloss.Color("#E0AF68") // orange
	colorMuted    = lipgloss.Color("#565F89")
	colorRunning  = lipgloss.Color("#9ECE6A")
	colorPaused   = lipgloss.Color("#F7768E")
	colorStar     = lipgloss.Color("#E0AF68")

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorTitle)

	footerStyle = lipgloss.NewStyle().Foreground(colorMuted)

	keyStyle = lipgloss.NewStyle().Bold(true).Foreground(colorTitle)
)

func panelColor(c scanner.Category) lipgloss.Color {
	switch c {
	case scanner.TopStars:
		return colorTopStars
	case scanner.WeeklyTrending:
		return colorWeekly
	default:
		return colorMonthly
	}
}

func panelTitle(c scanner.Category) string {
	switch c {
	case scanner.TopStars:
		return "★ Most Starred Repos"
	case scanner.WeeklyTrending:
		return "📈 Trending — Created in Last 7 Days"
	default:
		return "📈 Trending — Created in Last 30 Days"
	}
}

func panelSubtitle(c scanner.Category) string {
	switch c {
	case scanner.WeeklyTrending:
		return "sorted by total stars (heuristic proxy for weekly star gain)"
	case scanner.MonthlyTrending:
		return "sorted by total stars (heuristic proxy for monthly star gain)"
	default:
		return "sorted by total stars, all-time"
	}
}

type panelState struct {
	repos   []github.Repo
	err     error
	updated time.Time
}

// Model is the Bubble Tea application model.
type Model struct {
	scanner *scanner.Scanner
	cancel  context.CancelFunc

	panels map[scanner.Category]*panelState
	paused bool
	quit   bool

	width, height int
}

// New builds a Model wired to the given scanner and its cancel func, which
// is invoked to kill the background polling on quit.
func New(s *scanner.Scanner, cancel context.CancelFunc) Model {
	return Model{
		scanner: s,
		cancel:  cancel,
		panels: map[scanner.Category]*panelState{
			scanner.TopStars:        {},
			scanner.WeeklyTrending:  {},
			scanner.MonthlyTrending: {},
		},
		// Sane defaults in case the terminal never reports a WindowSizeMsg.
		width:  80,
		height: 24,
	}
}

type resultMsg scanner.Result
type statusMsg scanner.StatusMsg
type channelsClosedMsg struct{}

func waitForResult(ch <-chan scanner.Result) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return channelsClosedMsg{}
		}
		return resultMsg(r)
	}
}

func waitForStatus(ch <-chan scanner.StatusMsg) tea.Cmd {
	return func() tea.Msg {
		st, ok := <-ch
		if !ok {
			return channelsClosedMsg{}
		}
		return statusMsg(st)
	}
}

// Init starts listening to the scanner's channels.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		waitForResult(m.scanner.Results()),
		waitForStatus(m.scanner.Status()),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.cancel()
			m.quit = true
			return m, tea.Quit
		case "x":
			m.paused = m.scanner.TogglePause()
			return m, nil
		}
		return m, nil

	case resultMsg:
		p := m.panels[msg.Category]
		p.updated = msg.At
		if msg.Err != nil {
			p.err = msg.Err
		} else {
			p.err = nil
			p.repos = mergeSorted(p.repos, msg.Repos)
		}
		return m, waitForResult(m.scanner.Results())

	case statusMsg:
		m.paused = msg.Paused
		return m, waitForStatus(m.scanner.Status())

	case channelsClosedMsg:
		return m, nil
	}

	return m, nil
}

// mergeSorted replaces the panel's repo set with the freshly polled repos,
// deduplicated by full name and sorted by star count descending, capped at
// listSize. This is how newly-discovered repos get added and the list
// re-sorts on every poll.
func mergeSorted(existing []github.Repo, fresh []github.Repo) []github.Repo {
	byName := make(map[string]github.Repo, len(existing)+len(fresh))
	for _, r := range existing {
		byName[r.FullName] = r
	}
	for _, r := range fresh {
		byName[r.FullName] = r
	}

	merged := make([]github.Repo, 0, len(byName))
	for _, r := range byName {
		merged = append(merged, r)
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Stars > merged[j].Stars
	})
	if len(merged) > listSize {
		merged = merged[:listSize]
	}
	return merged
}

func (m Model) View() string {
	if m.quit {
		return ""
	}

	header := m.renderHeader()
	footer := m.renderFooter()

	availableHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - 1
	if availableHeight < 5 {
		availableHeight = 5
	}

	colWidth := (m.width - 8) / 3
	if colWidth < 20 {
		colWidth = 20
	}

	top := m.renderPanel(scanner.TopStars, colWidth, availableHeight)
	weekly := m.renderPanel(scanner.WeeklyTrending, colWidth, availableHeight)
	monthly := m.renderPanel(scanner.MonthlyTrending, colWidth, availableHeight)

	row := lipgloss.JoinHorizontal(lipgloss.Top, top, weekly, monthly)

	return lipgloss.JoinVertical(lipgloss.Left, header, row, footer)
}

func (m Model) renderHeader() string {
	status := lipgloss.NewStyle().Bold(true).Foreground(colorRunning).Render("● scanning")
	if m.paused {
		status = lipgloss.NewStyle().Bold(true).Foreground(colorPaused).Render("■ paused")
	}
	title := titleStyle.Render("GitHub Star Radar")
	return lipgloss.NewStyle().Padding(0, 1).Render(title + "   " + status)
}

func (m Model) renderFooter() string {
	help := fmt.Sprintf(
		"%s stop/resume scanning    %s quit (kills background scan)",
		keyStyle.Render("[x]"),
		keyStyle.Render("[q]"),
	)
	return footerStyle.Padding(0, 1).Render(help)
}

func (m Model) renderPanel(c scanner.Category, width, height int) string {
	color := panelColor(c)
	p := m.panels[c]

	var b strings.Builder
	b.WriteString(titleStyle.Foreground(color).Render(panelTitle(c)))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render(panelSubtitle(c)))
	b.WriteString("\n\n")

	switch {
	case p.err != nil:
		b.WriteString(lipgloss.NewStyle().Foreground(colorPaused).Render("error: " + p.err.Error()))
	case len(p.repos) == 0:
		b.WriteString(footerStyle.Render("waiting for results..."))
	default:
		for i, r := range p.repos {
			b.WriteString(renderRepoLine(i+1, r, width-4))
			b.WriteString("\n")
		}
	}

	if !p.updated.IsZero() {
		b.WriteString("\n")
		b.WriteString(footerStyle.Render("updated " + humanAgo(p.updated)))
	}

	return panelStyle.BorderForeground(color).Width(width).Height(height).Render(b.String())
}

func renderRepoLine(rank int, r github.Repo, width int) string {
	rankPlain := fmt.Sprintf("%2d.", rank)
	starPlain := fmt.Sprintf("★ %d", r.Stars)

	// Budget the name to whatever's left after the rank prefix, the two
	// single-space separators, and the star suffix, so the whole line
	// never wraps inside the panel border.
	maxNameLen := width - len(rankPlain) - 1 - 2 - len(starPlain)
	name := []rune(r.FullName)
	if maxNameLen < 3 {
		maxNameLen = 3
	}
	if len(name) > maxNameLen {
		name = append(name[:maxNameLen-1], '…')
	}

	star := lipgloss.NewStyle().Foreground(colorStar).Render(starPlain)
	rankStr := lipgloss.NewStyle().Foreground(colorMuted).Render(rankPlain)
	return fmt.Sprintf("%s %s  %s", rankStr, string(name), star)
}

func humanAgo(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm ago", int(d.Minutes()))
}
