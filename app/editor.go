package app

import (
	"claude-squad/session"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// editorOpenedMsg reports that VS Code was asked to open dir.
type editorOpenedMsg struct {
	dir string
}

// openEditor opens inst's worktree in VS Code, in the background.
func (m *home) openEditor(inst *session.Instance) tea.Cmd {
	dir, err := inst.EditorDir()
	if err != nil {
		return m.handleError(err)
	}
	return func() tea.Msg {
		if err := openInVSCode(dir); err != nil {
			return err
		}
		return editorOpenedMsg{dir: dir}
	}
}

// openInVSCode opens dir in VS Code on the machine claude-squad runs on. On macOS it asks
// macOS to open it, which also brings VS Code to the front: since macOS 14 an app started by
// VS Code's own code command may not take the focus, and its window stays behind the terminal.
// Elsewhere, or without the app in its usual place, it runs code. It is a variable for tests.
var openInVSCode = func(dir string) error {
	if runtime.GOOS == "darwin" {
		if exec.Command("open", "-a", "Visual Studio Code", dir).Run() == nil {
			return nil
		}
	}
	code, err := exec.LookPath("code")
	if err != nil {
		return fmt.Errorf("could not open VS Code: its code command is not on this machine's PATH")
	}
	if out, err := exec.Command(code, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("could not open VS Code: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}
