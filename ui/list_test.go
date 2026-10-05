package ui

import (
	"claude-squad/session"
	"claude-squad/session/claudestatus"
	"claude-squad/session/git"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
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
	r := &InstanceRenderer{spinner: &s}
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
