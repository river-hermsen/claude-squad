package session

import (
	"claude-squad/session/claudestatus"
	"fmt"
	"os"
	"syscall"
	"time"
)

// SetInstalledClaudeVersion records the version of Claude Code that the instance's program
// runs when started now (claudestatus.InstalledVersion). Call it from the main event loop.
func (i *Instance) SetInstalledClaudeVersion(version string) {
	i.installedClaude = version
}

// InstalledClaudeVersion returns the version of Claude Code a restart would run, or "".
func (i *Instance) InstalledClaudeVersion() string {
	return i.installedClaude
}

// UpdateAvailable reports whether the instance runs an older Claude Code than the installed
// one, which a restart (see StopForRestart) would run.
func (i *Instance) UpdateAvailable() bool {
	return i.claudeDir != "" && i.claudeProcess != nil &&
		claudestatus.OlderVersion(i.claudeProcess.Version, i.installedClaude)
}

// RestartWarnings says what restarting the instance would cut short, one sentence each: the
// turn Claude is working on, shell commands it runs in the background, or what is typed into
// its prompt. It returns nil if nothing. Call it from the main event loop.
func (i *Instance) RestartWarnings() []string {
	var warnings []string
	status := ""
	if i.claudeProcess != nil {
		status = i.claudeProcess.Status
	}
	switch {
	case status == "idle":
	case status == "shell":
		warnings = append(warnings, "Shell commands run in its background; they stop.")
	case status != "" || i.Status == Running:
		warnings = append(warnings, "Claude is working; that stops.")
	}
	if i.tmuxSession != nil {
		if content, err := i.tmuxSession.CapturePaneContent(); err == nil && !claudePromptIsEmpty(content) {
			warnings = append(warnings, "Its prompt is not empty; what is typed there is lost.")
		}
	}
	return warnings
}

// Restarting reports whether the instance is between StopForRestart and FinishRestart.
func (i *Instance) Restarting() bool {
	return i.restarting
}

// StopForRestart is the first half of restarting a running Claude instance, which then runs
// the installed Claude Code and continues its conversation. It closes the tmux session, which
// ends Claude Code, and leaves the instance Loading until FinishRestart starts it again.
//
// wait blocks until the ended process has exited, for at most a few seconds, so it no longer
// writes to the conversation when the new one continues it. Call wait from a background
// goroutine, then FinishRestart from the main event loop.
func (i *Instance) StopForRestart() (wait func(), err error) {
	if !i.started || i.restarting || i.Status == Paused || i.Status == Loading || i.tmuxSession == nil {
		return nil, fmt.Errorf("session '%s' is not running", i.Title)
	}
	if i.claudeDir == "" {
		return nil, fmt.Errorf("only Claude Code sessions can be restarted")
	}
	// Continue the conversation the pane shows right now.
	i.SetClaudeProcess(i.ComputeClaudeProcess())
	pid := i.tmuxSession.PanePID()
	if err := i.tmuxSession.Close(); err != nil && i.tmuxSession.DoesSessionExist() {
		return nil, fmt.Errorf("failed to stop session '%s': %w", i.Title, err)
	}
	i.restarting = true
	i.claudeProcess = nil
	i.SetStatus(Loading)
	return func() { waitForExit(pid, 5*time.Second) }, nil
}

// FinishRestart starts the instance StopForRestart stopped again: on the installed Claude
// Code, continuing its conversation if Claude Code saved it. If that fails, the instance is
// paused, so Resume can try again. Call it from the main event loop.
func (i *Instance) FinishRestart() error {
	if !i.restarting {
		return nil
	}
	i.restarting = false
	if err := i.writeClaudeSettings(); err != nil {
		i.SetStatus(Paused)
		return err
	}
	if err := i.restart(); err != nil {
		i.SetStatus(Paused)
		return fmt.Errorf("failed to restart session '%s': %w", i.Title, err)
	}
	i.SetStatus(Running)
	return nil
}

// waitForExit waits until process pid has exited, for at most timeout. Without a pid, it waits
// a second, about as long as Claude Code takes to exit.
func waitForExit(pid int, timeout time.Duration) {
	if pid <= 0 {
		time.Sleep(time.Second)
		return
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		// Signal 0 checks that the process exists without signalling it.
		if p.Signal(syscall.Signal(0)) != nil {
			return
		}
	}
}
