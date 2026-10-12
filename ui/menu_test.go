package ui

import (
	"claude-squad/keys"
	"claude-squad/session"
	"testing"

	"github.com/stretchr/testify/require"
)

// A paused session offers resume, in place or not: its tmux session can die like any other.
func TestMenuOffersResumeForPausedInstance(t *testing.T) {
	// Paused, so FromInstanceData does not try to reattach to a real tmux session.
	instance, err := session.FromInstanceData(session.InstanceData{
		Title:   "inplace",
		Path:    t.TempDir(),
		Status:  session.Paused,
		Program: "claude",
	})
	require.NoError(t, err)
	require.True(t, instance.InPlace())

	m := NewMenu()
	m.SetInstance(instance)

	require.Contains(t, m.options, keys.KeyResume)
	require.Contains(t, m.options, keys.KeyKill)
}

// A menu wider than the terminal drops the descriptions outside the highlighted group.
func TestMenuDropsDescriptionsWhenTooWide(t *testing.T) {
	instance, err := session.FromInstanceData(session.InstanceData{
		Title: "inplace", Path: t.TempDir(), Status: session.Paused, Program: "claude",
	})
	require.NoError(t, err)
	instance.SetStatus(session.Running)

	m := NewMenu()
	m.SetInstance(instance)
	require.Contains(t, m.options, keys.KeyEditor)
	m.SetSize(400, 1)
	require.Contains(t, m.String(), "n new • C resume claude")
	require.Contains(t, m.String(), "v vs code")

	m.SetSize(100, 1)
	require.Contains(t, m.String(), "n • C • D • R • f │ ↵/o open • →/` type • v vs code │ tab • J/K │ ? • q")
}

// → types into the session, except in the diff tab of a session that changed several
// repositories, where ←/→ switch between them.
func TestArrowsSwitchRepos(t *testing.T) {
	multi := newMultiRepoInstance(t, backendDiff, spaDiff)
	require.True(t, ArrowsSwitchRepos(DiffTab, multi))
	require.False(t, ArrowsSwitchRepos(PreviewTab, multi))
	require.False(t, ArrowsSwitchRepos(DiffTab, newMultiRepoInstance(t, backendDiff)), "one repository")
	require.False(t, ArrowsSwitchRepos(DiffTab, nil))

	m := NewMenu()
	multi.SetStatus(session.Running)
	m.SetInstance(multi)
	require.Contains(t, m.options, keys.KeyFocusRight)
	m.SetActiveTab(DiffTab)
	require.Contains(t, m.options, keys.KeyFocus)
	require.NotContains(t, m.options, keys.KeyFocusRight)
}
