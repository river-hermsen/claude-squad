package ui

import (
	"claude-squad/session"
	"claude-squad/session/git"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

var (
	repoTabStyle = lipgloss.NewStyle().Padding(0, 1).
			Foreground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#aaaaaa"})
	selectedRepoTabStyle = lipgloss.NewStyle().Padding(0, 1).Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#7D56F4"))
	repoHintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"})
)

type DiffPane struct {
	viewport viewport.Model
	// diff is the rendered diff, stats the header above it.
	diff   string
	stats  string
	width  int
	height int

	// raw is the git diff output that diff was rendered from, at renderedWidth. The diff is
	// refreshed many times a second, so rendering is skipped when neither changed.
	raw           string
	renderedWidth int

	// For multi-repo instances, the diff shows one repository at a time: repos are the
	// repositories with changes, selectedRepo the one shown. Selection is by name so it
	// survives refreshes.
	repos        []string
	selectedRepo string
}

func NewDiffPane() *DiffPane {
	return &DiffPane{
		viewport: viewport.New(0, 0),
	}
}

func (d *DiffPane) SetSize(width, height int) {
	d.width = width
	d.height = height
	d.viewport.Width = width
	d.viewport.Height = height
	// Update viewport content if diff exists
	if d.diff != "" || d.stats != "" {
		d.setRaw(d.raw)
		d.viewport.SetContent(lipgloss.JoinVertical(lipgloss.Left, d.stats, d.diff))
	}
}

func (d *DiffPane) SetDiff(instance *session.Instance) {
	centeredFallbackMessage := lipgloss.Place(
		d.width,
		d.height,
		lipgloss.Center,
		lipgloss.Center,
		"No changes",
	)

	if instance == nil || !instance.Started() {
		d.viewport.SetContent(centeredFallbackMessage)
		return
	}

	if !instance.IsMultiRepo() {
		d.repos = nil
	}

	if instance.InPlace() {
		d.viewport.SetContent(lipgloss.Place(
			d.width,
			d.height,
			lipgloss.Center,
			lipgloss.Center,
			fmt.Sprintf("No diff: %s is not a git repository", instance.Path),
		))
		return
	}

	stats := instance.GetDiffStats()
	if stats == nil {
		// Show loading message if worktree is not ready
		centeredMessage := lipgloss.Place(
			d.width,
			d.height,
			lipgloss.Center,
			lipgloss.Center,
			"Setting up worktree...",
		)
		d.viewport.SetContent(centeredMessage)
		return
	}

	if stats.Error != nil {
		// Show error message
		centeredMessage := lipgloss.Place(
			d.width,
			d.height,
			lipgloss.Center,
			lipgloss.Center,
			fmt.Sprintf("Error: %v", stats.Error),
		)
		d.viewport.SetContent(centeredMessage)
		return
	}

	if instance.IsMultiRepo() {
		d.setRepoDiffs(stats.Repos, centeredFallbackMessage)
		return
	}

	if stats.IsEmpty() {
		d.stats = ""
		d.diff = ""
		d.viewport.SetContent(centeredFallbackMessage)
	} else {
		files := strings.Count(stats.Content, "diff --git ")
		summary := lipgloss.NewStyle().Foreground(diffAddedFg).Render(fmt.Sprintf("+%d", stats.Added)) + " " +
			lipgloss.NewStyle().Foreground(diffRemovedFg).Render(fmt.Sprintf("-%d", stats.Removed)) +
			repoHintStyle.Render(fmt.Sprintf(" in %d %s", files, plural(files, "file", "files")))
		d.stats = summary + "\n"
		d.setRaw(stats.Content)
		d.viewport.SetContent(lipgloss.JoinVertical(lipgloss.Left, d.stats, d.diff))
	}
}

// setRaw renders raw git diff output into d.diff, unless it already holds that rendering.
func (d *DiffPane) setRaw(raw string) {
	if raw == d.raw && d.width == d.renderedWidth && d.diff != "" {
		return
	}
	d.raw = raw
	d.renderedWidth = d.width
	d.diff = renderDiff(raw, d.width)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// setRepoDiffs shows one repository's diff, under a bar listing every repository with changes.
func (d *DiffPane) setRepoDiffs(repos []git.RepoDiff, emptyMessage string) {
	d.repos = d.repos[:0]
	for _, r := range repos {
		d.repos = append(d.repos, r.Name)
	}
	if len(repos) == 0 {
		d.stats = ""
		d.diff = ""
		d.viewport.SetContent(emptyMessage)
		return
	}

	selected := repos[0]
	for _, r := range repos {
		if r.Name == d.selectedRepo {
			selected = r
		}
	}
	d.selectedRepo = selected.Name

	d.stats = d.repoBar(repos) + "\n"
	d.setRaw(selected.Content)
	d.viewport.SetContent(lipgloss.JoinVertical(lipgloss.Left, d.stats, d.diff))
}

// repoBar renders the repository selector. When the repositories do not fit on one line,
// it shows only the selected one and its position.
func (d *DiffPane) repoBar(repos []git.RepoDiff) string {
	hint := ""
	if len(repos) > 1 {
		hint = repoHintStyle.Render("  ←/→ switch repo")
	}

	tabs := make([]string, len(repos))
	position := 0
	for i, r := range repos {
		style := repoTabStyle
		if r.Name == d.selectedRepo {
			style = selectedRepoTabStyle
			position = i
		}
		tabs[i] = style.Render(fmt.Sprintf("%s +%d,-%d", r.Name, r.Added, r.Removed))
	}
	bar := strings.Join(tabs, " ")
	if lipgloss.Width(bar)+lipgloss.Width(hint) <= d.width {
		return bar + hint
	}

	r := repos[position]
	return selectedRepoTabStyle.Render(fmt.Sprintf("‹ %d/%d › %s +%d,-%d", position+1, len(repos), r.Name, r.Added, r.Removed)) + hint
}

// SwitchRepo moves the repository selection by delta, wrapping around. It takes effect on
// the next SetDiff.
func (d *DiffPane) SwitchRepo(delta int) {
	if len(d.repos) < 2 {
		return
	}
	idx := 0
	for i, name := range d.repos {
		if name == d.selectedRepo {
			idx = i
		}
	}
	idx = ((idx+delta)%len(d.repos) + len(d.repos)) % len(d.repos)
	d.selectedRepo = d.repos[idx]
	d.viewport.GotoTop()
}

func (d *DiffPane) String() string {
	return d.viewport.View()
}

// ScrollUp scrolls the viewport up
func (d *DiffPane) ScrollUp() {
	d.viewport.LineUp(1)
}

// ScrollDown scrolls the viewport down
func (d *DiffPane) ScrollDown() {
	d.viewport.LineDown(1)
}
