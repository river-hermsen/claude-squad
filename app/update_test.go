package app

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session"
	"claude-squad/session/claudestatus"
	"claude-squad/session/tmux"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

// U asks before restarting a session on the installed Claude Code, and offers to restart
// every idle session that has an update waiting.
func TestUpdateRestartsSessions(t *testing.T) {
	m := newTestHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	const emptyPrompt = "⏺ done\n──────\n❯ \n──────\n"
	add := func(title, status string) *session.Instance {
		inst, err := session.FromInstanceData(session.InstanceData{Title: title, Path: t.TempDir(), Program: "claude", Status: session.Paused})
		require.NoError(t, err)
		inst.SetStatus(session.Ready)
		inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps(title, "claude", nil, cmd_test.MockCmdExec{
			RunFunc: func(*exec.Cmd) error { return nil },
			OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
				if strings.Contains(cmd.String(), "capture-pane") {
					return []byte(emptyPrompt), nil
				}
				return nil, nil
			},
		}))
		inst.SetClaudeProcess(&claudestatus.Process{SessionID: "conv-" + title, Version: "2.1.296", Status: status})
		inst.SetInstalledClaudeVersion("2.1.297")
		m.list.AddInstance(inst)
		return inst
	}
	first, second, busy := add("first", "idle"), add("second", "idle"), add("busy", "busy")
	m.instanceChanged()

	press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U")})
	require.Equal(t, stateConfirm, m.state)
	require.Contains(t, m.confirmationOverlay.Render(), "Restart 'first' on Claude Code 2.1.297?")

	_, cmd := m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	require.NotNil(t, cmd, "the restarts finish once the old processes exit")
	require.Equal(t, stateDefault, m.state)
	for _, inst := range []*session.Instance{first, second} {
		require.True(t, inst.Restarting(), inst.Title)
		require.Equal(t, session.Loading, inst.Status, inst.Title)
	}
	require.False(t, busy.Restarting(), "a busy session is only restarted on its own")

	// U on the busy one warns that it stops Claude's work.
	m.list.SetSelectedInstance(2)
	press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U")})
	require.Equal(t, stateConfirm, m.state)
	require.Contains(t, m.confirmationOverlay.Render(), "Claude is working")
	require.NotContains(t, m.confirmationOverlay.Render(), "restart all", "no other idle session with an update")
	press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	require.False(t, busy.Restarting())
}
