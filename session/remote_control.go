package session

import (
	"claude-squad/session/claudestatus"
	"fmt"
	"time"
)

// Remote Control connects a Claude Code session to a session on claude.ai, so claude.ai/code
// and the Claude app can continue it while it keeps running here. Claude Code records the
// claude.ai session in its registry of running sessions, which the metadata tick reads; see
// SetClaudeProcess.

// RemoteState is the state of an instance's Remote Control.
type RemoteState int

const (
	// RemoteOff means Remote Control is off and not asked for.
	RemoteOff RemoteState = iota
	// RemoteStarting means Remote Control is asked for and not connected yet, or the instance
	// is paused and connects again when resumed.
	RemoteStarting
	// RemoteFailed means Remote Control is asked for but still not connected well after the
	// session started, as when Claude Code could not connect or it was disconnected.
	RemoteFailed
	// RemoteOn means the session is connected to claude.ai.
	RemoteOn
)

// remoteGrace is how long Claude Code gets to connect Remote Control before RemoteState
// reports it failed.
const remoteGrace = 45 * time.Second

// resumeGrace is how soon after a restart on an earlier conversation the session must end
// for MarkEnded to take it that Claude Code could not continue the conversation.
const resumeGrace = 15 * time.Second

// RemoteState reports the state of the instance's Remote Control.
func (i *Instance) RemoteState() RemoteState {
	switch {
	case !i.IsClaude():
		return RemoteOff
	case i.remoteSessionID != "" && i.started && i.Status != Paused:
		return RemoteOn
	case !i.RemoteControl:
		return RemoteOff
	case !i.started || i.Status == Paused || i.Status == Loading || i.pendingRemoteControl:
		return RemoteStarting
	case time.Since(i.remoteSince) > remoteGrace:
		return RemoteFailed
	default:
		return RemoteStarting
	}
}

// RemoteURL returns the address where claude.ai/code and the Claude app continue the
// session, or "" while Remote Control is not connected.
func (i *Instance) RemoteURL() string {
	if i.RemoteState() != RemoteOn {
		return ""
	}
	return claudestatus.RemoteURL(i.remoteSessionID)
}

// SetClaudeProcess records Claude Code's registry entry of the process in the instance's
// tmux pane (see ComputeClaudeProcess): its Remote Control session and its conversation, which
// a restart continues. Without an entry, what was recorded stays. It reports whether the
// conversation changed, which is worth saving. Call it from the main event loop.
func (i *Instance) SetClaudeProcess(p *claudestatus.Process) (conversationChanged bool) {
	if p == nil {
		return false
	}
	i.remoteSessionID = p.RemoteSessionID
	if p.RemoteSessionID != "" {
		i.pendingRemoteControl = false
	}
	if p.SessionID != "" && p.SessionID != i.claudeSessionID {
		i.claudeSessionID = p.SessionID
		return true
	}
	return false
}

// EnableRemoteControl turns Remote Control on: in the running session by sending it
// `/remote-control` once it is idle (see SyncRemoteControl), and on every later start.
func (i *Instance) EnableRemoteControl() error {
	if !i.IsClaude() {
		return fmt.Errorf("session '%s' runs %s: Remote Control needs Claude Code", i.Title, i.Program)
	}
	i.RemoteControl = true
	i.remoteSince = time.Now()
	i.pendingRemoteControl = i.started && i.Status != Paused && i.remoteSessionID == ""
	if i.tmuxSession != nil {
		i.tmuxSession.SetProgram(i.launchProgram())
	}
	return nil
}

// SyncRemoteControl sends `/remote-control` to a session EnableRemoteControl switched on,
// once Claude Code is waiting with an empty prompt, so it never mixes with what the user is
// typing; until then it reports false and waits for the next call. Call it from the main event
// loop when the instance is idle.
func (i *Instance) SyncRemoteControl() (bool, error) {
	if !i.pendingRemoteControl || !i.started || i.Status == Paused || i.tmuxSession == nil {
		return false, nil
	}
	if i.remoteSessionID != "" {
		// Already connected: the command would open Claude Code's Remote Control panel.
		i.pendingRemoteControl = false
		return false, nil
	}
	content, err := i.tmuxSession.CapturePaneContent()
	if err != nil || !claudePromptIsEmpty(content) {
		return false, err
	}
	i.pendingRemoteControl = false
	i.remoteSince = time.Now()
	return true, i.SendPrompt("/remote-control")
}

// DisableRemoteControl keeps later starts of the session from turning Remote Control on. If
// it is connected now, it opens Claude Code's Remote Control panel, where the user picks
// "Disconnect this session", and reports true. Like SyncRemoteControl, it only types into an
// empty prompt.
func (i *Instance) DisableRemoteControl() (panelOpened bool, err error) {
	i.RemoteControl = false
	i.pendingRemoteControl = false
	if i.tmuxSession != nil {
		i.tmuxSession.SetProgram(i.launchProgram())
	}
	if i.RemoteState() != RemoteOn {
		return false, nil
	}
	content, err := i.tmuxSession.CapturePaneContent()
	if err != nil {
		return false, err
	}
	if !claudePromptIsEmpty(content) {
		return false, fmt.Errorf("session '%s' is busy or has a draft in its prompt: disconnect it with /remote-control there", i.Title)
	}
	return true, i.SendPrompt("/remote-control")
}

// MarkEnded pauses an instance whose tmux session ended, as when the program in it exits, so
// it can be resumed. If it ended right after Resume restarted it on its earlier conversation,
// Claude Code could not continue that conversation; then the next restart starts a new one,
// and the error says so.
func (i *Instance) MarkEnded() error {
	i.SetStatus(Paused)
	i.remoteSessionID = ""
	i.pendingRemoteControl = false
	if i.resumedAt.IsZero() || time.Since(i.resumedAt) > resumeGrace {
		return nil
	}
	i.resumedAt = time.Time{}
	i.claudeSessionID = ""
	return fmt.Errorf("session '%s' exited right after continuing its conversation; press r to start a new one", i.Title)
}
