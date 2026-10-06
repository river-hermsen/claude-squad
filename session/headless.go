package session

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// These help `cs new` start an instance without the TUI: it waits for Claude Code to be ready
// before it types the task, and for Remote Control to connect before it prints the link.

// ErrNeedsTrust means Claude Code asks whether to trust the session's folder, which someone
// has to answer in the session.
var ErrNeedsTrust = errors.New("Claude Code asks whether to trust the folder")

// pollInterval is how often the waits below look at the session.
var pollInterval = 250 * time.Millisecond

// WaitUntilReady waits until Claude Code in a started instance shows its empty prompt, ready
// for a task, for at most timeout. It returns ErrNeedsTrust if Claude Code asks to trust the
// folder instead, which it must not get keys meant for the prompt: Enter there means "No, exit".
// Other programs are taken to be ready at once.
func (i *Instance) WaitUntilReady(timeout time.Duration) error {
	if !i.IsClaude() {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for {
		content, err := i.tmuxSession.CapturePaneContent()
		if err != nil && !i.tmuxSession.DoesSessionExist() {
			return fmt.Errorf("Claude Code exited while starting")
		}
		if err == nil {
			if claudePromptIsEmpty(content) {
				return nil
			}
			if strings.Contains(content, "trust this folder") || strings.Contains(content, "Do you trust the files") {
				return ErrNeedsTrust
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Claude Code was not ready after %s", timeout)
		}
		time.Sleep(pollInterval)
	}
}

// SendTask types prompt into the session and submits it. A prompt of several lines goes in as
// one paste, so its line breaks do not submit it early.
func (i *Instance) SendTask(prompt string) error {
	if !strings.ContainsAny(prompt, "\r\n") {
		return i.SendPrompt(prompt)
	}
	return i.SendPrompt("\x1b[200~" + prompt + "\x1b[201~")
}

// WaitForRemoteURL waits until Remote Control has connected the instance's Claude Code
// session to claude.ai, for at most timeout, and returns the link, or "" if it did not connect
// in time. Not for instances the TUI shows: it records what it reads.
func (i *Instance) WaitForRemoteURL(timeout time.Duration) string {
	if !i.IsClaude() {
		return ""
	}
	deadline := time.Now().Add(timeout)
	for {
		i.SetClaudeProcess(i.ComputeClaudeProcess())
		if url := i.RemoteURL(); url != "" || time.Now().After(deadline) {
			return url
		}
		time.Sleep(pollInterval)
	}
}
