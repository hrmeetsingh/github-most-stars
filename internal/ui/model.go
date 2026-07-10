// Package ui implements the Bubble Tea terminal UI: three colored panels
// (top stars, weekly trending, monthly trending) fed by a background
// scanner, plus a status/help footer and an animated progress bar.
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

const listSize = 25

// progressTickInterval drives the indeterminate progress bar animation.
const progressTickInterval = 150 * time.Millisecond

// panelOrder fixes the left-to-right, Tab-cycle order of the three panels.
var panelOrder = []scanner.Category{
	scanner.TopStars,
	scanner.WeeklyTrending,
	scanner.MonthlyTrending,
}

var (
	colorTitle       = lipgloss.Color("#F5F5F5")
	colorTopStars    = lipgloss.Color("#7AA2F7") // blue
	colorWeekly      = lipgloss.Color("#9ECE6A") // green
	colorMonthly     = lipgloss.Color("#E0AF68") // orange
	colorMuted       = lipgloss.Color("#565F89")
	colorRunning     = lipgloss.Color("#9ECE6A")
	colorPaused      = lipgloss.Color("#F7768E")
	colorStar        = lipgloss.Color("#E0AF68")
	colorFocusBorder = lipgloss.Color("#3B82F6") // blue focus outline
	colorSelected    = lipgloss.Color("#ADD8E6") // light blue selected repo name

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
		return "Most Starred Repos"
	case scanner.WeeklyTrending:
		return "Trending — Created in Last 7 Days"
	default:
		return "Trending — Created in Last 30 Days"
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

	focused  scanner.Category
	selected map[scanner.Category]int

	progressFrame int

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
		focused: scanner.TopStars,
		selected: map[scanner.Category]int{
			scanner.TopStars:        0,
			scanner.WeeklyTrending:  0,
			scanner.MonthlyTrending: 0,
		},
		// Sane defaults in case the terminal never reports a WindowSizeMsg.
		width:  80,
		height: 24,
	}
}

type resultMsg scanner.Result
type statusMsg scanner.StatusMsg
type channelsClosedMsg struct{}
type progressTickMsg time.Time

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

func tickProgress() tea.Cmd {
	return tea.Tick(progressTickInterval, func(t time.Time) tea.Msg {
		return progressTickMsg(t)
	})
}

// Init starts listening to the scanner's channels and the progress
// animation ticker.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		waitForResult(m.scanner.Results()),
		waitForStatus(m.scanner.Status()),
		tickProgress(),
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
		case "tab":
			m.focused = nextCategory(m.focused, 1)
			return m, nil
		case "shift+tab":
			m.focused = nextCategory(m.focused, -1)
			return m, nil
		case "up":
			m.moveSelection(-1)
			return m, nil
		case "down":
			m.moveSelection(1)
			return m, nil
		case "enter":
			return m, m.openSelected()
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

	case progressTickMsg:
		m.progressFrame++
		return m, tickProgress()

	case channelsClosedMsg:
		return m, nil
	}

	return m, nil
}

func nextCategory(c scanner.Category, dir int) scanner.Category {
	idx := 0
	for i, o := range panelOrder {
		if o == c {
			idx = i
			break
		}
	}
	n := len(panelOrder)
	idx = ((idx+dir)%n + n) % n
	return panelOrder[idx]
}

func (m *Model) moveSelection(delta int) {
	p := m.panels[m.focused]
	if len(p.repos) == 0 {
		return
	}
	sel := m.selected[m.focused] + delta
	if sel < 0 {
		sel = 0
	}
	if sel > len(p.repos)-1 {
		sel = len(p.repos) - 1
	}
	m.selected[m.focused] = sel
}

func (m Model) openSelected() tea.Cmd {
	p := m.panels[m.focused]
	sel := m.selected[m.focused]
	if sel < 0 || sel >= len(p.repos) {
		return nil
	}
	url := p.repos[sel].HTMLURL
	return func() tea.Msg {
		_ = openBrowser(url)
		return nil
	}
}

// mergeSorted replaces the panel's repo set with the freshly polled repos,
// deduplicated by full name and sorted by star count descending, capped at
// listSize. This is how newly-discovered repos get added and the list
// re-sorts and keeps growing on every poll.
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

	totalHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - 1
	if totalHeight < 6 {
		totalHeight = 6
	}

	barHeight := totalHeight / 10
	if barHeight < 1 {
		barHeight = 1
	}
	panelHeight := totalHeight - barHeight
	if panelHeight < 5 {
		panelHeight = 5
	}

	colWidth := (m.width - 8) / 3
	if colWidth < 20 {
		colWidth = 20
	}

	top := m.renderPanel(scanner.TopStars, colWidth, panelHeight)
	weekly := m.renderPanel(scanner.WeeklyTrending, colWidth, panelHeight)
	monthly := m.renderPanel(scanner.MonthlyTrending, colWidth, panelHeight)

	row := lipgloss.JoinHorizontal(lipgloss.Top, top, weekly, monthly)
	bar := m.renderProgressBar(lipgloss.Width(row), barHeight)

	return lipgloss.JoinVertical(lipgloss.Left, header, row, bar, footer)
}

func (m Model) renderHeader() string {
	status := lipgloss.NewStyle().Bold(true).Foreground(colorRunning).Render("RUNNING")
	if m.paused {
		status = lipgloss.NewStyle().Bold(true).Foreground(colorPaused).Render("PAUSED")
	}
	title := titleStyle.Render("GitHub Star Radar")
	return lipgloss.NewStyle().Padding(0, 1).Render(title + "   " + status)
}

func (m Model) renderFooter() string {
	help := fmt.Sprintf(
		"%s stop/resume    %s switch section    %s move selection    %s open in browser    %s quit",
		keyStyle.Render("[x]"),
		keyStyle.Render("[tab]"),
		keyStyle.Render("[up/down]"),
		keyStyle.Render("[enter]"),
		keyStyle.Render("[q]"),
	)
	return footerStyle.Padding(0, 1).Render(help)
}

func (m Model) renderPanel(c scanner.Category, width, height int) string {
	color := panelColor(c)
	focused := c == m.focused
	p := m.panels[c]
	sel := m.selected[c]

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
			b.WriteString(renderRepoLine(i+1, r, width-4, i == sel, focused))
			b.WriteString("\n")
		}
	}

	if !p.updated.IsZero() {
		b.WriteString("\n")
		b.WriteString(footerStyle.Render("updated " + humanAgo(p.updated)))
	}

	style := panelStyle.Width(width).Height(height)
	if focused {
		style = style.BorderForeground(colorFocusBorder)
	} else {
		style = style.BorderForeground(color)
	}
	return style.Render(b.String())
}

func renderRepoLine(rank int, r github.Repo, width int, selected, focused bool) string {
	marker := "  "
	if focused && selected {
		marker = "> "
	}
	rankPlain := fmt.Sprintf("%2d.", rank)
	starPlain := fmt.Sprintf("%d stars", r.Stars)

	// Budget the name to whatever's left after the marker, the rank prefix,
	// the two single-space separators, and the star suffix, so the whole
	// line never wraps inside the panel border.
	maxNameLen := width - len(marker) - len(rankPlain) - 1 - 2 - len(starPlain)
	name := []rune(r.FullName)
	if maxNameLen < 3 {
		maxNameLen = 3
	}
	if len(name) > maxNameLen {
		name = append(name[:maxNameLen-1], '…')
	}

	nameStyle := lipgloss.NewStyle()
	if selected {
		nameStyle = nameStyle.Foreground(colorSelected)
	}
	nameStr := nameStyle.Render(string(name))
	if r.HTMLURL != "" {
		nameStr = hyperlink(r.HTMLURL, nameStr)
	}

	star := lipgloss.NewStyle().Foreground(colorStar).Render(starPlain)
	rankStr := lipgloss.NewStyle().Foreground(colorMuted).Render(rankPlain)
	markerStr := lipgloss.NewStyle().Foreground(colorFocusBorder).Render(marker)
	return fmt.Sprintf("%s%s %s  %s", markerStr, rankStr, nameStr, star)
}

// hyperlink wraps text in an OSC 8 escape sequence so terminals that
// support it (iTerm2, Kitty, WezTerm, modern Windows Terminal, etc.) render
// it as a clickable link pointing at url.
func hyperlink(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// renderProgressBar draws an indeterminate "background work in progress"
// bar spanning width, ping-ponging a highlighted segment across it. When
// paused, the bar is rendered dim and static.
func (m Model) renderProgressBar(width, height int) string {
	if width < 4 {
		width = 4
	}

	track := make([]rune, width)
	for i := range track {
		track[i] = '─'
	}

	color := colorRunning
	label := "background scan in progress"
	if m.paused {
		color = colorMuted
		label = "background scan paused"
	} else {
		segLen := width / 6
		if segLen < 3 {
			segLen = 3
		}
		span := width - segLen
		if span < 1 {
			span = 1
		}
		period := span * 2
		pos := m.progressFrame % period
		if pos > span {
			pos = period - pos
		}
		for i := pos; i < pos+segLen && i < width; i++ {
			track[i] = '█'
		}
	}

	line := lipgloss.NewStyle().Foreground(color).Render(string(track))
	rows := []string{line}
	for len(rows) < height {
		rows = append(rows, footerStyle.Render(label))
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(strings.Join(rows, "\n"))
}

func humanAgo(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm ago", int(d.Minutes()))
}
