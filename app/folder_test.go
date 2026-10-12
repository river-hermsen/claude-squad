package app

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"claude-squad/ui"
	"os/exec"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

// newNamingTestHome returns a home naming a new instance that starts in launchDir.
func newNamingTestHome(t *testing.T, launchDir string) (*home, *session.Instance) {
	m := newTestHome(t)
	m.launchDir = launchDir
	inst, err := session.NewInstance(session.InstanceOptions{Path: launchDir, Program: "claude"})
	require.NoError(t, err)
	m.list.AddInstance(inst)
	m.list.SetSelectedInstance(0)
	m.state = stateNew
	m.menu.SetState(ui.StateNewInstance)
	return m, inst
}

func press(t *testing.T, m *home, msg tea.KeyMsg) {
	t.Helper()
	// The first press of a menu key only highlights it and sends it again.
	m.keySent = false
	_, _ = m.handleKeyPress(msg)
	if m.keySent {
		_, _ = m.handleKeyPress(msg)
	}
}

func typeKeys(t *testing.T, m *home, text string) {
	t.Helper()
	for _, r := range text {
		press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// Tab while naming a new session picks the folder it starts in; the name typed so far stays.
func TestTabPicksFolderOfNewInstance(t *testing.T) {
	launchDir, other := t.TempDir(), t.TempDir()
	m, inst := newNamingTestHome(t, launchDir)
	typeKeys(t, m, "fix")

	press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, stateFolder, m.state)
	require.Contains(t, m.View(), "Start the new session in")

	typeKeys(t, m, other)
	press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, stateNew, m.state)
	require.Nil(t, m.folderPicker)
	require.Equal(t, other, inst.Path)
	require.Equal(t, "fix", inst.Title)

	// Esc leaves the folder as it was and the instance still being named.
	press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, stateNew, m.state)
	require.Equal(t, other, inst.Path)
	require.Equal(t, 1, m.list.NumInstances())
}

func TestRecentFoldersStartWithInstanceAndLaunchDir(t *testing.T) {
	m, inst := newNamingTestHome(t, "/src/foys-all")
	require.NoError(t, inst.SetPath("/src/claude-squad"))
	other, err := session.NewInstance(session.InstanceOptions{Title: "other", Path: "/src/notes", Program: "claude"})
	require.NoError(t, err)
	m.list.AddInstance(other)

	require.Equal(t, []string{"/src/claude-squad", "/src/foys-all", "/src/notes"}, m.recentFolders(inst.Path))
}

// C asks for the folder first: Claude's picker lists the conversations of the folder the
// session starts in. Enter on the first folder keeps the launch directory.
func TestResumeConversationPicksFolder(t *testing.T) {
	launchDir, other := t.TempDir(), t.TempDir()
	m := newTestHome(t)
	m.launchDir, m.program = launchDir, "claude"

	press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	require.Equal(t, stateFolder, m.state)
	require.Contains(t, m.View(), "Continue a Claude conversation from")
	require.Zero(t, m.list.NumInstances(), "nothing starts before a folder is chosen")

	typeKeys(t, m, other)
	press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, stateDefault, m.state)
	require.Equal(t, 1, m.list.NumInstances())
	inst := m.list.GetInstances()[0]
	require.Equal(t, other, inst.Path)
	require.True(t, inst.AdoptsClaudeName())
	require.Equal(t, session.Loading, inst.Status)

	// Esc starts nothing.
	press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, stateDefault, m.state)
	require.Equal(t, 1, m.list.NumInstances())

	press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, launchDir, m.list.GetInstances()[1].Path, "the launch directory comes first")
}

// → in the list types into the selected session, as ` does; ` stops.
func TestRightArrowStartsTyping(t *testing.T) {
	m := newTestHome(t)
	inst, err := session.FromInstanceData(session.InstanceData{Title: "typing", Path: t.TempDir(), Program: "claude", Status: session.Paused})
	require.NoError(t, err)
	inst.SetStatus(session.Running)
	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps("typing", "claude", nil, cmd_test.MockCmdExec{
		RunFunc:    func(*exec.Cmd) error { return nil },
		OutputFunc: func(*exec.Cmd) ([]byte, error) { return nil, nil },
	}))
	m.list.AddInstance(inst)
	m.instanceChanged()

	press(t, m, tea.KeyMsg{Type: tea.KeyRight})
	require.Equal(t, stateFocus, m.state)
	require.Contains(t, m.View(), "typing into typing")

	press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("`")})
	require.Equal(t, stateDefault, m.state)
}
