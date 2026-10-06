package app

import (
	"claude-squad/session"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

// remoteControl is KeyRemote on inst: with Remote Control connected, it copies the session's
// link and shows it with a QR code; otherwise it turns Remote Control on.
func (m *home) remoteControl(inst *session.Instance) tea.Cmd {
	if !inst.IsClaude() {
		return m.handleError(fmt.Errorf("session '%s' runs %s: Remote Control needs Claude Code", inst.Title, inst.Program))
	}
	if url := inst.RemoteURL(); url != "" {
		m.remoteOverlay = overlay.NewRemoteControlOverlay(inst.Title, url, copyToClipboard(url, os.Stdout), m.windowHeight)
		m.remoteInstance = inst
		m.state = stateRemote
		return nil
	}
	if inst.RemoteState() == session.RemoteStarting && !inst.Paused() {
		return m.showInfo(fmt.Sprintf("Remote Control for '%s' is still connecting", inst.Title))
	}
	if err := inst.EnableRemoteControl(); err != nil {
		return m.handleError(err)
	}
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		return m.handleError(err)
	}
	if inst.Paused() {
		return m.showInfo(fmt.Sprintf("Remote Control turns on when '%s' resumes (r)", inst.Title))
	}
	return m.showInfo(fmt.Sprintf("Turning on Remote Control for '%s' as soon as it is idle", inst.Title))
}

// handleRemoteKey handles a key while remoteOverlay is shown: every key closes it, and o and
// d open the link in a browser or disconnect the session.
func (m *home) handleRemoteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.remoteOverlay.HandleKeyPress(msg) {
		return m, nil
	}
	choice, inst := m.remoteOverlay.Choice(), m.remoteInstance
	m.remoteOverlay, m.remoteInstance = nil, nil
	m.state = stateDefault

	switch choice {
	case overlay.RemoteOpen:
		url := inst.RemoteURL()
		if url == "" {
			return m, m.handleError(fmt.Errorf("session '%s' is no longer connected to claude.ai", inst.Title))
		}
		if err := openBrowser(url); err != nil {
			return m, m.handleError(err)
		}
	case overlay.RemoteDisconnect:
		panelOpened, err := inst.DisableRemoteControl()
		if saveErr := m.storage.SaveInstances(m.list.GetInstances()); saveErr != nil && err == nil {
			err = saveErr
		}
		if err != nil {
			return m, m.handleError(err)
		}
		if !panelOpened {
			return m, m.showInfo(fmt.Sprintf("Remote Control stays off for '%s'", inst.Title))
		}
		// Claude Code's panel asks how to disconnect; keys go to it until ` is pressed.
		m.list.SelectInstance(inst)
		m.tabbedWindow.SetActiveTab(ui.PreviewTab)
		m.menu.SetActiveTab(ui.PreviewTab)
		m.state = stateFocus
		return m, tea.Batch(m.instanceChanged(), refreshPreviewSoon(),
			m.showInfo("Pick \"Disconnect this session\" in Claude's panel, then press ` to go back"))
	}
	return m, nil
}

// copyToClipboard copies text to the clipboard and returns a note saying where it went. It
// asks the terminal to copy it with OSC 52, written to out, which also reaches the clipboard
// of the computer an SSH session comes from. tmux drops that request from a program by
// default, so inside tmux, tmux itself is asked to pass it on. On a machine with a clipboard
// of its own, it copies there too.
func copyToClipboard(text string, out io.Writer) string {
	if err := writeClipboard(text); err == nil {
		return "Copied to the clipboard."
	}
	if os.Getenv("TMUX") != "" {
		load := exec.Command("tmux", "load-buffer", "-w", "-")
		load.Stdin = strings.NewReader(text)
		if err := load.Run(); err == nil {
			return "Sent to your clipboard if your terminal allows that; else select it and ⌘C."
		}
	}
	_, _ = osc52.New(text).WriteTo(out)
	return "Sent to your clipboard (OSC 52) if your terminal allows that; else select it and ⌘C."
}

// writeClipboard copies text to the clipboard of the machine claude-squad runs on. It is a
// variable for tests.
var writeClipboard = clipboard.WriteAll

// openBrowser opens url in the browser of the machine claude-squad runs on.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return fmt.Errorf("no browser on this machine: open the link on your laptop or phone")
		}
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open a browser: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
