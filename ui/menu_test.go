package ui

import (
	"claude-squad/keys"
	"claude-squad/session"
	"testing"

	"github.com/stretchr/testify/require"
)

// Push and checkout act on the session's branch, so they are hidden for sessions that run
// outside a git repository. Resume still applies: their tmux session can die like any other.
func TestMenuHidesGitActionsForInstanceWithoutWorktree(t *testing.T) {
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

	require.NotContains(t, m.options, keys.KeyCheckout)
	require.Contains(t, m.options, keys.KeyResume)
	require.Contains(t, m.options, keys.KeyKill)
}

// Multi-repo sessions keep checkout: it acts on every changed repository.
func TestMenuShowsGitActionsForMultiRepoInstance(t *testing.T) {
	instance, err := session.FromInstanceData(session.InstanceData{
		Title:     "multi",
		Path:      t.TempDir(),
		Status:    session.Paused,
		Program:   "claude",
		Workspace: &session.WorkspaceData{Root: t.TempDir(), Dir: t.TempDir(), BranchName: "me/multi"},
	})
	require.NoError(t, err)
	require.True(t, instance.IsMultiRepo())

	m := NewMenu()
	m.SetInstance(instance)
	require.Contains(t, m.options, keys.KeyResume)

	instance.SetStatus(session.Running)
	m.SetInstance(instance)
	require.Contains(t, m.options, keys.KeyCheckout)
}
