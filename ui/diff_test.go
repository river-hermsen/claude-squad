package ui

import (
	"claude-squad/keys"
	"claude-squad/session"
	"claude-squad/session/git"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

// newMultiRepoInstance returns a started (paused) multi-repo instance with the given diff.
func newMultiRepoInstance(t *testing.T, repos ...git.RepoDiff) *session.Instance {
	instance, err := session.FromInstanceData(session.InstanceData{
		Title:     "multi",
		Path:      t.TempDir(),
		Status:    session.Paused,
		Program:   "claude",
		Workspace: &session.WorkspaceData{Root: t.TempDir(), Dir: t.TempDir(), BranchName: "me/multi"},
	})
	require.NoError(t, err)
	stats := &git.DiffStats{Repos: repos}
	for _, r := range repos {
		stats.Added += r.Added
		stats.Removed += r.Removed
	}
	instance.SetDiffStats(stats)
	return instance
}

var (
	backendDiff = git.RepoDiff{Name: "backend", Added: 2, Removed: 1, Content: "diff --git a/b.cs b/b.cs\n--- a/b.cs\n+++ b/b.cs\n" +
		"@@ -1,2 +1,3 @@\n-old line\n+backend line\n+another line\n context\n"}
	spaDiff = git.RepoDiff{Name: "spa", Added: 1, Content: "diff --git a/s.ts b/s.ts\n--- a/s.ts\n+++ b/s.ts\n" +
		"@@ -1 +1,2 @@\n context\n+spa line\n"}
)

func TestDiffPaneShowsOneRepositoryAtATime(t *testing.T) {
	instance := newMultiRepoInstance(t, backendDiff, spaDiff)
	d := NewDiffPane()
	d.SetSize(120, 30)

	d.SetDiff(instance)
	out := d.String()
	require.Contains(t, out, "backend +2,-1")
	require.Contains(t, out, "spa +1,-0")
	require.Contains(t, out, "←/→ switch repo")
	require.Contains(t, out, "backend line")
	require.NotContains(t, out, "spa line")

	d.SwitchRepo(1)
	d.SetDiff(instance)
	require.Contains(t, d.String(), "spa line")
	require.NotContains(t, d.String(), "backend line")

	d.SwitchRepo(1)
	d.SetDiff(instance)
	require.Contains(t, d.String(), "backend line", "selection wraps around")

	d.SwitchRepo(-1)
	d.SetDiff(instance)
	require.Contains(t, d.String(), "spa line", "and wraps backwards")

	// The selection follows the repository, not its position, across refreshes.
	instance.SetDiffStats(&git.DiffStats{Added: 3, Removed: 1, Repos: []git.RepoDiff{spaDiff, backendDiff}})
	d.SetDiff(instance)
	require.Contains(t, d.String(), "spa line")
}

func TestDiffPaneRepoBarFallsBackToPositionWhenNarrow(t *testing.T) {
	instance := newMultiRepoInstance(t, backendDiff, spaDiff)
	d := NewDiffPane()
	d.SetSize(30, 30)

	d.SetDiff(instance)
	require.Contains(t, d.String(), "‹ 1/2 › backend")
}

func TestDiffPaneWithoutChangedRepositories(t *testing.T) {
	instance := newMultiRepoInstance(t)
	d := NewDiffPane()
	d.SetSize(60, 10)

	d.SetDiff(instance)
	require.Contains(t, d.String(), "No changes")
}

func TestMenuOffersRepoSwitchingInDiffTab(t *testing.T) {
	m := NewMenu()
	m.SetInstance(newMultiRepoInstance(t, backendDiff, spaDiff))
	require.NotContains(t, m.options, keys.KeyNextRepo, "only in the diff tab")

	m.SetActiveTab(DiffTab)
	require.Contains(t, m.options, keys.KeyNextRepo)
	require.Contains(t, m.String(), "n new • D kill │ ↵/o open • r resume │ shift+↑ scroll • ←/→ switch repo • tab switch tab │ ? help • q quit")

	m.SetInstance(newMultiRepoInstance(t, backendDiff))
	require.NotContains(t, m.options, keys.KeyNextRepo, "nothing to switch between")
}

func TestDiffPaneHighlightsSelectedRepository(t *testing.T) {
	// Tests run without a terminal, so styles render as plain text unless forced.
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	instance := newMultiRepoInstance(t, backendDiff, spaDiff)
	d := NewDiffPane()
	d.SetSize(120, 30)
	backend := selectedRepoTabStyle.Render("backend +2,-1")
	spa := selectedRepoTabStyle.Render("spa +1,-0")

	d.SetDiff(instance)
	require.Contains(t, d.String(), backend)
	require.NotContains(t, d.String(), spa)

	d.SwitchRepo(1)
	d.SetDiff(instance)
	require.Contains(t, d.String(), spa)
	require.NotContains(t, d.String(), backend)
}
