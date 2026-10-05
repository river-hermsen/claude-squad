package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/claudestatus"
	"claude-squad/session/git"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const readyIcon = "● "
const pausedIcon = "⏸ "

var readyStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var addedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var removedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#de613e"))

var pausedStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})

var titleStyle = lipgloss.NewStyle().
	Padding(0, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"})

var listDescStyle = lipgloss.NewStyle().
	Padding(0, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"})

var selectedTitleStyle = lipgloss.NewStyle().
	Padding(0, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var selectedDescStyle = lipgloss.NewStyle().
	Padding(0, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var mainTitle = lipgloss.NewStyle().
	Background(lipgloss.Color("62")).
	Foreground(lipgloss.Color("230"))

var autoYesStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.Color("#1a1a1a"))

type List struct {
	items         []*session.Instance
	selectedIdx   int
	height, width int
	renderer      *InstanceRenderer
	autoyes       bool
	// limits are the account's usage limits Claude Code reported last; nil before any did.
	limits *claudestatus.Limits
}

func NewList(spinner *spinner.Model, autoYes bool) *List {
	return &List{
		items:    []*session.Instance{},
		renderer: &InstanceRenderer{spinner: spinner},
		autoyes:  autoYes,
	}
}

// SetSize sets the height and width of the list.
func (l *List) SetSize(width, height int) {
	l.width = width
	l.height = height
	// One column of margin on the left, and one between the list and the tabbed window.
	l.renderer.setWidth(width - 2)
}

// SetSessionPreviewSize sets the height and width for the tmux sessions. This makes the stdout line have the correct
// width and height.
func (l *List) SetSessionPreviewSize(width, height int) (err error) {
	for i, item := range l.items {
		if !item.Started() || item.Paused() {
			continue
		}

		if innerErr := item.SetPreviewSize(width, height); innerErr != nil {
			err = errors.Join(
				err, fmt.Errorf("could not set preview size for instance %d: %v", i, innerErr))
		}
	}
	return
}

func (l *List) NumInstances() int {
	return len(l.items)
}

// InstanceRenderer handles rendering of session.Instance objects
type InstanceRenderer struct {
	spinner *spinner.Model
	width   int
}

// setWidth sets the width of a rendered item, minus the 2 columns of the item's padding.
func (r *InstanceRenderer) setWidth(width int) {
	r.width = width - 2
}

// Render renders an instance as a title row and an info row: the repositories it changed,
// the model, context use and effort Claude Code reports, and the diff counts. A selected
// multi-repo instance lists its changed repositories below that, in at most maxRepoRows rows.
func (r *InstanceRenderer) Render(i *session.Instance, idx int, selected bool, maxRepoRows int) string {
	prefix := fmt.Sprintf(" %d. ", idx)
	if idx >= 10 {
		prefix = prefix[:len(prefix)-1]
	}
	titleS := selectedTitleStyle
	descS := selectedDescStyle
	if !selected {
		titleS = titleStyle
		descS = listDescStyle
	}

	// add spinner next to title if it's running
	var join string
	switch i.Status {
	case session.Running, session.Loading:
		join = fmt.Sprintf("%s ", r.spinner.View())
	case session.Ready:
		join = readyStyle.Render(readyIcon)
	case session.Paused:
		join = pausedStyle.Render(pausedIcon)
	default:
	}

	// Cut the title if it's too long
	titleText := i.Title
	widthAvail := r.width - 3 - runewidth.StringWidth(prefix) - 1
	if widthAvail > 0 && runewidth.StringWidth(titleText) > widthAvail {
		titleText = runewidth.Truncate(titleText, widthAvail-3, "...")
	}
	title := titleS.Render(lipgloss.JoinHorizontal(
		lipgloss.Left,
		lipgloss.Place(r.width-3, 1, lipgloss.Left, lipgloss.Center, fmt.Sprintf("%s %s", prefix, titleText)),
		" ",
		join,
	))

	// Rows below the title line up with its text. Every part of them is drawn with base's
	// background, so a selected instance is tinted edge to edge.
	indent := runewidth.StringWidth(prefix) + 1
	base := lipgloss.NewStyle().Foreground(descS.GetForeground()).Background(descS.GetBackground())

	stat := i.GetDiffStats()
	var repos []git.RepoDiff
	var counts string
	if stat != nil && stat.Error == nil && !stat.IsEmpty() {
		repos = stat.Repos
		counts = diffCounts(stat.Added, stat.Removed, base)
	}

	info := agentSegments(i)
	switch {
	case !i.Started() || i.InPlace():
	case i.IsMultiRepo():
		if !selected && len(repos) > 0 {
			info = append([]segment{{text: fmt.Sprintf("%d %s", len(repos), plural(len(repos), "repo", "repos"))}}, info...)
		}
	default:
		if repo, err := i.RepoName(); err == nil {
			// A long repository name gives way to the agent details after it.
			room := r.width - indent - lipgloss.Width(counts) - 1 - segmentsWidth(info) - runewidth.StringWidth(rowSeparator)
			repo = runewidth.Truncate(repo, max(room, 8), "…")
			info = append([]segment{{text: repo}}, info...)
		}
	}

	rows := []string{title, descS.Render(r.row(indent, info, counts, base))}

	if selected && i.IsMultiRepo() && len(repos) > 0 {
		shown, more := repos, 0
		if len(repos) > maxRepoRows {
			shown = repos[:max(maxRepoRows-1, 0)]
			more = len(repos) - len(shown)
		}
		for _, repo := range shown {
			rows = append(rows, descS.Render(r.row(indent+2, []segment{{text: repo.Name}}, diffCounts(repo.Added, repo.Removed, base), base)))
		}
		if more > 0 {
			rows = append(rows, descS.Render(r.row(indent+2, []segment{{text: fmt.Sprintf("%d more", more)}}, "", base)))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// segment is a piece of an instance row, drawn in color or else in the row's own color.
type segment struct {
	text  string
	color lipgloss.TerminalColor
}

const rowSeparator = " · "

func segmentsWidth(segments []segment) int {
	width := 0
	for k, s := range segments {
		if k > 0 {
			width += runewidth.StringWidth(rowSeparator)
		}
		width += runewidth.StringWidth(s.text)
	}
	return width
}

// row renders one row r.width wide: indent, then left joined by separators and cut short
// if it does not fit, then right against the right edge.
func (r *InstanceRenderer) row(indent int, left []segment, right string, base lipgloss.Style) string {
	rightWidth := lipgloss.Width(right)
	if indent+rightWidth > r.width {
		right, rightWidth = "", 0
	}
	room := r.width - indent - rightWidth
	if rightWidth > 0 {
		room-- // keep a space before right
	}

	var b strings.Builder
	b.WriteString(base.Render(strings.Repeat(" ", indent)))
	used := 0
	write := func(text string, color lipgloss.TerminalColor) bool {
		if used+runewidth.StringWidth(text) > room {
			text = runewidth.Truncate(text, max(room-used, 0), "…")
		}
		style := base
		if color != nil {
			style = style.Foreground(color)
		}
		b.WriteString(style.Render(text))
		used += runewidth.StringWidth(text)
		return used < room
	}
	for k, s := range left {
		if k > 0 && !write(rowSeparator, nil) {
			break
		}
		if !write(s.text, s.color) {
			break
		}
	}

	if pad := r.width - indent - used - rightWidth; pad > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", pad)))
	}
	b.WriteString(right)
	return b.String()
}

// diffCounts renders "+added,-removed " in the colors of the diff.
func diffCounts(added, removed int, base lipgloss.Style) string {
	bg := base.GetBackground()
	return addedLinesStyle.Background(bg).Render(fmt.Sprintf("+%d", added)) +
		base.Render(",") +
		removedLinesStyle.Background(bg).Render(fmt.Sprintf("-%d", removed)) +
		base.Render(" ")
}

// agentSegments describes the agent: the model, context use and effort Claude Code last
// reported, or else the program's name.
func agentSegments(i *session.Instance) []segment {
	info := i.GetClaudeInfo()
	if info == nil || info.Model == "" {
		fields := strings.Fields(i.Program)
		if len(fields) == 0 {
			return nil
		}
		return []segment{{text: filepath.Base(fields[0])}}
	}

	// "Opus 4.6 (1M context)" repeats what the context size says.
	model, _, _ := strings.Cut(info.Model, " (")
	segments := []segment{{text: model}}
	if info.ContextSize > 0 {
		segments = append(segments, segment{
			text:  formatTokens(info.ContextUsed) + "/" + formatTokens(info.ContextSize),
			color: contextColor(info.ContextUsed, info.ContextSize),
		})
	}
	if info.Effort != "" {
		segments = append(segments, segment{text: info.Effort})
	}
	return segments
}

// formatTokens formats a token count the way Claude Code's status line does: 950, 92.1k, 200k, 1M.
func formatTokens(n int) string {
	short := func(v float64) string {
		return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0")
	}
	switch {
	case n >= 1_000_000:
		return short(float64(n)/1_000_000) + "M"
	case n >= 1000:
		return short(float64(n)/1000) + "k"
	default:
		return strconv.Itoa(n)
	}
}

// contextColor warns when the context window is filling up: amber with less than half
// left, red with less than a fifth.
func contextColor(used, size int) lipgloss.TerminalColor {
	left := float64(size-used) / float64(size)
	switch {
	case left < 0.2:
		return removedLinesStyle.GetForeground()
	case left < 0.5:
		return lipgloss.Color("#c08400")
	default:
		return nil
	}
}

func (l *List) String() string {
	const titleText = " Instances "
	const autoYesText = " auto-yes "

	// Write the title, on the same row as the tab names next to it.
	var b strings.Builder
	b.WriteString("\n")

	// Write the title line, as wide as the items: the title, then on the right the account's
	// usage limits and the auto-yes badge.
	titleWidth := l.width - 2
	title := mainTitle.Render(titleText)
	var badge string
	if l.autoyes {
		badge = " " + autoYesStyle.Render(autoYesText)
	}
	room := titleWidth - lipgloss.Width(title) - lipgloss.Width(badge) - 2
	right := renderLimits(l.limits, time.Now(), room) + badge
	b.WriteString(title)
	if pad := titleWidth - lipgloss.Width(title) - lipgloss.Width(right); pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	b.WriteString(right)

	b.WriteString("\n")
	b.WriteString("\n")

	// The selected instance's repository rows get whatever height the list has left: the
	// title takes 3 rows, every instance 2 plus a blank row between them.
	maxRepoRows := max(l.height-3-3*len(l.items)+1, 1)

	// Render the list.
	for i, item := range l.items {
		b.WriteString(l.renderer.Render(item, i+1, i == l.selectedIdx, maxRepoRows))
		if i != len(l.items)-1 {
			b.WriteString("\n\n")
		}
	}
	return lipgloss.Place(l.width, l.height, lipgloss.Left, lipgloss.Top,
		lipgloss.NewStyle().PaddingLeft(1).Render(b.String()))
}

// SetLimits sets the account's usage limits shown in the title row.
func (l *List) SetLimits(limits *claudestatus.Limits) {
	l.limits = limits
}

// renderLimits renders limits in at most width columns, as "5h 12% ↻15:00 · wk 40% ↻Fri",
// dropping the reset times if they do not fit, and everything if that does not fit either.
// Before Claude Code has reported any, it shows where they will appear: "5h – · wk –".
func renderLimits(limits *claudestatus.Limits, now time.Time, width int) string {
	if limits == nil {
		dim := listDescStyle.UnsetPadding()
		if text := dim.Render("5h – · wk –"); lipgloss.Width(text) <= width {
			return text
		}
		return ""
	}
	for _, withResets := range []bool{true, false} {
		text := renderLimit("5h", limits.FiveHour, now, withResets, "15:04") +
			listDescStyle.UnsetPadding().Render(" · ") +
			renderLimit("wk", limits.Week, now, withResets, "Mon")
		if lipgloss.Width(text) <= width {
			return text
		}
	}
	return ""
}

// renderLimit renders one usage limit, colored by how much of it is used. resetLayout formats
// the reset time when it is more than a day away; closer resets show the time of day.
func renderLimit(label string, limit claudestatus.Limit, now time.Time, withReset bool, resetLayout string) string {
	used := limit.UsedPercent
	reset := ""
	if !limit.ResetsAt.IsZero() {
		if now.After(limit.ResetsAt) {
			// The window has reset since Claude Code reported it.
			used = 0
		} else if withReset {
			layout := resetLayout
			if limit.ResetsAt.Sub(now) < 24*time.Hour {
				layout = "15:04"
			}
			reset = " ↻" + limit.ResetsAt.Local().Format(layout)
		}
	}

	color := readyStyle.GetForeground()
	switch {
	case used >= 80:
		color = removedLinesStyle.GetForeground()
	case used >= 50:
		color = lipgloss.Color("#c08400")
	}
	dim := listDescStyle.UnsetPadding()
	return dim.Render(label+" ") + lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("%.0f%%", used)) + dim.Render(reset)
}

// Down selects the next item in the list.
func (l *List) Down() {
	if len(l.items) == 0 {
		return
	}
	if l.selectedIdx < len(l.items)-1 {
		l.selectedIdx++
	} else {
		l.selectedIdx = 0
	}
}

// Kill selects the next item in the list.
func (l *List) Kill() {
	if len(l.items) == 0 {
		return
	}
	targetInstance := l.items[l.selectedIdx]

	// Kill the tmux session
	if err := targetInstance.Kill(); err != nil {
		log.ErrorLog.Printf("could not kill instance: %v", err)
	}

	// If you delete the last one in the list, select the previous one.
	if l.selectedIdx == len(l.items)-1 {
		defer l.Up()
	}

	// Since there's items after this, the selectedIdx can stay the same.
	l.items = append(l.items[:l.selectedIdx], l.items[l.selectedIdx+1:]...)
}

func (l *List) Attach() (chan struct{}, error) {
	targetInstance := l.items[l.selectedIdx]
	return targetInstance.Attach()
}

// Up selects the prev item in the list.
func (l *List) Up() {
	if len(l.items) == 0 {
		return
	}
	if l.selectedIdx > 0 {
		l.selectedIdx--
	} else {
		l.selectedIdx = len(l.items) - 1
	}
}

// AddInstance adds a new instance to the end of the list.
func (l *List) AddInstance(instance *session.Instance) {
	l.items = append(l.items, instance)
}

// GetSelectedInstance returns the currently selected instance
func (l *List) GetSelectedInstance() *session.Instance {
	if len(l.items) == 0 {
		return nil
	}
	return l.items[l.selectedIdx]
}

// SetSelectedInstance sets the selected index. Noop if the index is out of bounds.
func (l *List) SetSelectedInstance(idx int) {
	if idx >= len(l.items) {
		return
	}
	l.selectedIdx = idx
}

// SelectInstance finds and selects the given instance in the list.
func (l *List) SelectInstance(target *session.Instance) {
	for i, inst := range l.items {
		if inst == target {
			l.SetSelectedInstance(i)
			return
		}
	}
}

// MoveUp swaps the selected instance with the one above it.
func (l *List) MoveUp() bool {
	if l.selectedIdx <= 0 || len(l.items) < 2 {
		return false
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx-1] = l.items[l.selectedIdx-1], l.items[l.selectedIdx]
	l.selectedIdx--
	return true
}

// MoveDown swaps the selected instance with the one below it.
func (l *List) MoveDown() bool {
	if l.selectedIdx >= len(l.items)-1 || len(l.items) < 2 {
		return false
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx+1] = l.items[l.selectedIdx+1], l.items[l.selectedIdx]
	l.selectedIdx++
	return true
}

// GetInstances returns all instances in the list
func (l *List) GetInstances() []*session.Instance {
	return l.items
}
