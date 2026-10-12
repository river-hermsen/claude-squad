package app

import (
	"claude-squad/session"
	"claude-squad/session/git"
	"claude-squad/ui/overlay"

	tea "github.com/charmbracelet/bubbletea"
)

// askFolder shows the folder picker, titled title, which offers current first. Once a folder
// is chosen, onPick gets it; Esc calls onCancel instead. Both leave the state they want.
func (m *home) askFolder(title string, current string, onPick func(dir string) tea.Cmd, onCancel func() tea.Cmd) {
	m.folderPicker = overlay.NewFolderPicker(title, m.launchDir, m.recentFolders(current))
	if m.windowWidth > 0 {
		m.folderPicker.SetWidth(min(max(m.windowWidth*6/10, 50), 90))
	}
	m.onFolderPick, m.onFolderCancel = onPick, onCancel
	m.state = stateFolder
}

// pickFolder asks for the folder inst, the instance being named, starts in, and goes back to
// naming it.
func (m *home) pickFolder(inst *session.Instance) {
	m.askFolder("Start the new session in", inst.Path, func(dir string) tea.Cmd {
		m.state = stateNew
		if err := inst.SetPath(dir); err != nil {
			return m.handleError(err)
		}
		if m.promptAfterName {
			return fetchBranchesCmd(inst.Path)
		}
		return nil
	}, func() tea.Cmd {
		m.state = stateNew
		return nil
	})
}

// recentFolders returns the folders the picker offers before anything is typed: current, the
// launch directory, then those of the instances in the list.
func (m *home) recentFolders(current string) []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(dir string) {
		if dir != "" && !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	add(current)
	add(m.launchDir)
	for _, other := range m.list.GetInstances() {
		add(other.Path)
	}
	return dirs
}

// handleFolderKey passes a key to the folder picker, and once it is done hands the chosen
// folder on, or reports that it was canceled.
func (m *home) handleFolderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "ctrl+c" && !m.folderPicker.HandleKeyPress(msg) {
		return m, nil
	}
	picker, onPick, onCancel := m.folderPicker, m.onFolderPick, m.onFolderCancel
	m.folderPicker, m.onFolderPick, m.onFolderCancel = nil, nil, nil
	m.state = stateDefault
	if picker.Submitted() {
		return m, onPick(picker.Value())
	}
	return m, onCancel()
}

// fetchBranchesCmd fetches the branches of the repository in dir in the background, if dir is
// in one, so they are up to date by the time the branch picker opens.
func fetchBranchesCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		if git.IsGitRepo(dir) {
			git.FetchBranches(dir)
		}
		return nil
	}
}
