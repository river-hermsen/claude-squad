package ui

import (
	"claude-squad/session"
	"claude-squad/session/claudestatus"
	"claude-squad/session/git"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

func newTestList(titles ...string) *List {
	s := spinner.New()
	l := NewList(&s, false)
	for _, t := range titles {
		inst, _ := session.NewInstance(session.InstanceOptions{
			Title:   t,
			Path:    ".",
			Program: "echo",
		})
		l.AddInstance(inst)
	}
	return l
}

func TestMoveUp(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveUp()
	require.True(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "b", l.items[0].Title)
	require.Equal(t, "a", l.items[1].Title)
	require.Equal(t, "c", l.items[2].Title)
}

func TestMoveUp_AtTop(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(0)

	moved := l.MoveUp()
	require.False(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "a", l.items[0].Title)
}

func TestMoveDown(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveDown()
	require.True(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "a", l.items[0].Title)
	require.Equal(t, "c", l.items[1].Title)
	require.Equal(t, "b", l.items[2].Title)
}

func TestMoveDown_AtBottom(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(2)

	moved := l.MoveDown()
	require.False(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "c", l.items[2].Title)
}

func TestMoveWithSingleItem(t *testing.T) {
	l := newTestList("only")
	l.SetSelectedInstance(0)

	require.False(t, l.MoveUp())
	require.False(t, l.MoveDown())
}

// newRenderTestInstance returns a started (paused) Claude instance with diff counts for two
// repositories and a status from Claude Code. workspace is nil for an in-place instance.
func newRenderTestInstance(t *testing.T, program string, workspace *session.WorkspaceData) *session.Instance {
	t.Setenv("HOME", t.TempDir())
	inst, err := session.FromInstanceData(session.InstanceData{
		Title: "test-edit", Path: "/src/foys-all", Program: program, Status: session.Paused, Workspace: workspace,
	})
	require.NoError(t, err)
	inst.SetDiffStats(&git.DiffStats{Added: 4, Removed: 1, Repos: []git.RepoDiff{
		{Name: "foys-backend", Added: 3},
		{Name: "foys-booking-module-backend", Added: 1, Removed: 1},
	}})
	inst.SetClaudeInfo(&claudestatus.Info{Model: "Opus 4.7 (1M context)", Effort: "xhigh", ContextUsed: 92100, ContextSize: 1_000_000})
	return inst
}

var multiRepoWorkspace = &session.WorkspaceData{Root: "/src/foys-all", Dir: "/ws/test-edit", BranchName: "me/test-edit"}

func renderRows(t *testing.T, inst *session.Instance, width int, selected bool, maxRepoRows int) []string {
	t.Helper()
	s := spinner.New()
	r := &InstanceRenderer{spinner: &s, launchDir: "/src/foys-all"}
	r.setWidth(width)
	rows := strings.Split(r.Render(inst, 1, selected, maxRepoRows), "\n")
	for _, row := range rows {
		require.Equal(t, width, lipgloss.Width(row), "row %q", row)
	}
	return rows
}

func TestRenderCountsReposOfUnselectedInstance(t *testing.T) {
	inst := newRenderTestInstance(t, "claude", multiRepoWorkspace)

	rows := renderRows(t, inst, 60, false, 10)
	require.Len(t, rows, 2)
	require.Equal(t, "      2 repos · Opus 4.7 · 92.1k/1M · xhigh          +4,-1  ", rows[1])
	require.NotContains(t, rows[1], "foys-all", "no directory and no branch icon")
}

func TestRenderListsReposOfSelectedInstance(t *testing.T) {
	inst := newRenderTestInstance(t, "claude", multiRepoWorkspace)

	rows := renderRows(t, inst, 60, true, 10)
	require.Equal(t, []string{
		"      Opus 4.7 · 92.1k/1M · xhigh                    +4,-1  ",
		"        foys-backend                                 +3,-0  ",
		"        foys-booking-module-backend                  +1,-1  ",
	}, rows[1:])

	rows = renderRows(t, inst, 60, true, 1)
	require.Equal(t, "        2 more", strings.TrimRight(rows[2], " "), "rows past the list height are summed up")
}

func TestRenderSingleRepoAndInPlaceInstances(t *testing.T) {
	single := &session.WorkspaceData{Root: "/src", Dir: "/ws/test-edit", BranchName: "me/test-edit", SingleRepo: "foys-court-booking-module-management-spa"}
	rows := renderRows(t, newRenderTestInstance(t, "claude", single), 60, true, 10)
	require.Len(t, rows, 2, "a single repo needs no list")
	require.Equal(t, "      foys-court-book… · Opus 4.7 · 92.1k/1M · xhigh +4,-1  ", rows[1],
		"a long repository name gives way to the model")

	inPlace := newRenderTestInstance(t, "claude", nil)
	inPlace.SetDiffStats(nil)
	rows = renderRows(t, inPlace, 60, false, 10)
	require.Equal(t, "      Opus 4.7 · 92.1k/1M · xhigh", strings.TrimRight(rows[1], " "))

	codex := newRenderTestInstance(t, "/opt/bin/codex --full-auto", nil)
	codex.SetDiffStats(nil)
	codex.SetClaudeInfo(nil)
	rows = renderRows(t, codex, 60, false, 10)
	require.Equal(t, "      codex", strings.TrimRight(rows[1], " "), "other agents show their name")
}

// Sessions in another folder than claude-squad's own name it, as does one being named.
func TestRenderFolderOutsideLaunchDir(t *testing.T) {
	elsewhere := &session.WorkspaceData{Root: "/src/other-repos", Dir: "/ws/test-edit", BranchName: "me/test-edit"}
	inst, err := session.FromInstanceData(session.InstanceData{
		Title: "test-edit", Path: "/src/other-repos", Program: "claude", Status: session.Paused, Workspace: elsewhere,
	})
	require.NoError(t, err)
	inst.SetDiffStats(&git.DiffStats{Added: 1, Repos: []git.RepoDiff{{Name: "a", Added: 1}}})
	rows := renderRows(t, inst, 60, false, 10)
	require.Equal(t, "      other-repos · 1 repo · claude", strings.TrimRight(rows[1], " +1,-0"))

	inPlace, err := session.FromInstanceData(session.InstanceData{
		Title: "test-edit", Path: "/tmp/notes", Program: "claude", Status: session.Paused,
	})
	require.NoError(t, err)
	rows = renderRows(t, inPlace, 60, false, 10)
	require.Equal(t, "      notes · claude", strings.TrimRight(rows[1], " "))

	naming, err := session.NewInstance(session.InstanceOptions{Path: "/src/foys-all", Program: "claude"})
	require.NoError(t, err)
	rows = renderRows(t, naming, 60, true, 10)
	require.Equal(t, "      foys-all · claude", strings.TrimRight(rows[1], " "))
}

func TestRenderCutsInfoToWidth(t *testing.T) {
	inst := newRenderTestInstance(t, "claude", multiRepoWorkspace)
	rows := renderRows(t, inst, 30, false, 10)
	require.Equal(t, "      2 repos · Opus … +4,-1  ", rows[1])
}

func TestFormatTokens(t *testing.T) {
	for n, want := range map[int]string{0: "0", 950: "950", 92_100: "92.1k", 200_000: "200k", 1_000_000: "1M", 1_500_000: "1.5M"} {
		require.Equal(t, want, formatTokens(n))
	}
}

func TestRenderLimits(t *testing.T) {
	now := time.Date(2026, 10, 5, 13, 0, 0, 0, time.Local)
	limits := &claudestatus.Limits{
		FiveHour: claudestatus.Limit{UsedPercent: 12.4, ResetsAt: now.Add(2 * time.Hour)},
		Week:     claudestatus.Limit{UsedPercent: 81, ResetsAt: time.Date(2026, 10, 9, 9, 0, 0, 0, time.Local)},
	}
	require.Equal(t, "5h 12% ↻15:00 · wk 81% ↻Fri", renderLimits(limits, now, 40))
	require.Equal(t, "5h 12% · wk 81%", renderLimits(limits, now, 20), "reset times go first")
	require.Empty(t, renderLimits(limits, now, 10))
	require.Equal(t, "5h 0% · wk 81% ↻Fri", renderLimits(limits, now.Add(3*time.Hour), 40), "a passed reset means nothing used")
	require.Equal(t, "5h – · wk –", renderLimits(nil, now, 40), "where the limits will show")
}

// The title row shows the limits on the right, after the title.
func TestListTitleShowsLimits(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)
	l.SetSize(60, 20)
	title := strings.Split(l.String(), "\n")[1]
	require.True(t, strings.HasSuffix(strings.TrimRight(title, " "), "5h – · wk –"), "title row: %q", title)

	l.SetLimits(&claudestatus.Limits{FiveHour: claudestatus.Limit{UsedPercent: 9}, Week: claudestatus.Limit{UsedPercent: 31}})
	title = strings.Split(l.String(), "\n")[1]
	require.Contains(t, title, "Instances")
	require.True(t, strings.HasSuffix(strings.TrimRight(title, " "), "5h 9% · wk 31%"), "title row: %q", title)
}

// An instance's title row ends in its status icon and keeps its width, selected or not; a long
// title gives way.
func TestRenderInstanceTitleRow(t *testing.T) {
	inst := newRenderTestInstance(t, "claude", nil)
	inst.SetStatus(session.Paused)
	title := func(selected bool) string {
		t.Helper()
		s := spinner.New()
		r := &InstanceRenderer{spinner: &s}
		r.setWidth(40)
		row := strings.Split(r.Render(inst, 1, selected, 10), "\n")[0]
		require.Equal(t, 40, lipgloss.Width(row), "row %q", row)
		return ansi.Strip(row)
	}
	require.True(t, strings.HasSuffix(strings.TrimRight(title(false), " "), " ⏸"), title(false))
	require.True(t, strings.HasSuffix(strings.TrimRight(title(true), " "), " ⏸"), "selected: %q", title(true))

	inst.Title = strings.Repeat("long title ", 4)
	require.Contains(t, title(false), "...")
	require.Contains(t, title(false), " ⏸")
}

// While keys go to the selected instance it takes the color of the preview's border, and its
// colored details give way to the row's own color.
func TestRenderTypingInstance(t *testing.T) {
	defer lipgloss.SetColorProfile(lipgloss.ColorProfile())
	lipgloss.SetColorProfile(termenv.TrueColor)
	inst := newRenderTestInstance(t, "claude", multiRepoWorkspace)
	inst.SetClaudeInfo(&claudestatus.Info{Model: "Opus 5.5", ContextUsed: 700_000, ContextSize: 1_000_000})
	render := func(typing, selected bool) string {
		s := spinner.New()
		r := &InstanceRenderer{spinner: &s, launchDir: "/src/foys-all", typing: typing}
		r.setWidth(60)
		return r.Render(inst, 1, selected, 10)
	}
	isFocus := func(out string) bool {
		return strings.Contains(out, "48;2;245;158;11") || strings.Contains(out, "48;2;217;119;6")
	}
	const amber = "38;2;192;13" // contextColor #c08400, with less than half left

	browsing := render(false, true)
	require.False(t, isFocus(browsing))
	require.Contains(t, browsing, amber)

	typing := render(true, true)
	require.True(t, isFocus(typing), "the focus color: %q", typing)
	require.NotContains(t, typing, amber, "it would not show on the focus color")
	require.Equal(t, ansi.Strip(browsing), ansi.Strip(typing), "the same text")

	require.False(t, isFocus(render(true, false)), "only the selected instance")
}

// A list taller than its pane scrolls to keep the selected instance in view, moving only when
// the selection leaves it, and says how many instances are out of view.
func TestListScrollsToSelected(t *testing.T) {
	titles := make([]string, 20)
	for i := range titles {
		titles[i] = fmt.Sprintf("session-%d", i+1)
	}
	l := newTestList(titles...)
	// 17 rows for instances: 5 of them, and the row saying more follow.
	l.SetSize(40, 20)
	view := func() string {
		t.Helper()
		out := l.String()
		require.Equal(t, l.height, lipgloss.Height(out), "the list keeps its height")
		return ansi.Strip(out)
	}
	rows := func() []string { return strings.Split(view(), "\n") }

	out := view()
	require.Contains(t, out, "session-5")
	require.NotContains(t, out, "session-6")
	require.NotContains(t, out, "↑")
	require.Contains(t, out, "↓ 15 more")

	l.SetSelectedInstance(4)
	require.Contains(t, view(), "session-1 ", "still in view: no scrolling")

	l.SetSelectedInstance(5)
	out = view()
	require.NotContains(t, out, "session-1 ")
	require.Contains(t, out, "session-6")
	require.Equal(t, "↑ 1 more", strings.TrimSpace(rows()[2]), "the row under the title")
	require.Contains(t, out, "↓ 14 more")

	l.SetSelectedInstance(19)
	out = view()
	require.Contains(t, out, "↑ 14 more")
	require.Contains(t, out, "session-15")
	require.Contains(t, out, "session-20")
	require.NotContains(t, out, "↓")

	// Moving up inside the window does not scroll.
	l.SetSelectedInstance(15)
	require.Contains(t, view(), "↑ 14 more")

	// With room for all of them, the instances above come back.
	l.SetSize(40, 70)
	out = view()
	require.Contains(t, out, "session-1 ")
	require.NotContains(t, out, "↑")
	require.NotContains(t, out, "↓")
}

// A session on an older Claude Code than the installed one says an update is waiting.
func TestRenderUpdateAvailable(t *testing.T) {
	inst := newRenderTestInstance(t, "claude", nil)
	inst.SetDiffStats(nil)
	inst.SetClaudeProcess(&claudestatus.Process{Version: "2.1.296"})
	rows := renderRows(t, inst, 60, false, 10)
	require.Equal(t, "      Opus 4.7 · 92.1k/1M · xhigh", strings.TrimRight(rows[1], " "), "installed version unknown")

	inst.SetInstalledClaudeVersion("2.1.297")
	rows = renderRows(t, inst, 60, false, 10)
	require.Equal(t, "      Opus 4.7 · 92.1k/1M · xhigh · ⬆ update", strings.TrimRight(rows[1], " "))
}
