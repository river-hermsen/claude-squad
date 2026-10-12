package app

import (
	"claude-squad/session"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

// v opens the selected session's worktree in VS Code, and says so.
func TestEditorKeyOpensVSCode(t *testing.T) {
	m := newTestHome(t)
	dir := t.TempDir()
	inst, err := session.FromInstanceData(session.InstanceData{Title: "notes", Path: dir, Program: "claude", Status: session.Paused})
	require.NoError(t, err)
	m.list.AddInstance(inst)

	var opened string
	orig := openInVSCode
	openInVSCode = func(dir string) error { opened = dir; return nil }
	t.Cleanup(func() { openInVSCode = orig })

	m.keySent = true // past the menu highlighting
	_, cmd := m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	require.NotNil(t, cmd)
	msg := cmd()
	require.Equal(t, editorOpenedMsg{dir: dir}, msg)
	require.Equal(t, dir, opened)

	m.Update(msg)
	require.Contains(t, m.errBox.String(), "in VS Code")
}
