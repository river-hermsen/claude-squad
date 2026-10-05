package session

import (
	"claude-squad/session/claudestatus"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newRemoteInstance starts an in-place Claude instance with Remote Control on, whose pane runs
// process 50251, and returns where Claude Code's registry entry of that process goes.
func newRemoteInstance(t *testing.T, title string) (*Instance, *fakeTmux, string) {
	t.Setenv("HOME", t.TempDir())
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	instance, fake := newInPlaceInstance(t, title)
	fake.panePID = "50251"
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, "sessions"), 0755))
	return instance, fake, filepath.Join(claudeDir, "sessions", "50251.json")
}

// register writes Claude Code's registry entry of the instance's process, as Claude Code does.
func register(t *testing.T, path, entry string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(entry), 0644))
}

// saveTranscript makes Claude Code's saved conversation sessionID exist.
func saveTranscript(t *testing.T, sessionID string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-some-project")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, sessionID+".jsonl"), []byte("{}\n"), 0644))
}

// A Claude session started with Remote Control launches with --remote-control, and the list
// learns from Claude Code's registry when it is connected, and where.
func TestRemoteControlStateFollowsRegistry(t *testing.T) {
	instance, _, entry := newRemoteInstance(t, "remote")
	require.NotContains(t, instance.launchProgram(), "--remote-control")
	require.Equal(t, RemoteOff, instance.RemoteState())

	instance.RemoteControl = true
	require.Contains(t, instance.launchProgram(), "claude --name 'remote' --remote-control --settings ")
	require.Equal(t, RemoteStarting, instance.RemoteState(), "Claude Code is still connecting")
	require.Empty(t, instance.RemoteURL())

	register(t, entry, `{"pid":50251,"sessionId":"conv-1","bridgeSessionId":"session_01ABC"}`)
	require.True(t, instance.SetClaudeProcess(instance.ComputeClaudeProcess()), "a new conversation is worth saving")
	require.Equal(t, RemoteOn, instance.RemoteState())
	require.Equal(t, "https://claude.ai/code/session_01ABC", instance.RemoteURL())
	require.False(t, instance.SetClaudeProcess(instance.ComputeClaudeProcess()), "same conversation")

	// Disconnecting from Claude Code's panel sets the entry to null.
	register(t, entry, `{"pid":50251,"sessionId":"conv-1","bridgeSessionId":null}`)
	instance.SetClaudeProcess(instance.ComputeClaudeProcess())
	require.Empty(t, instance.RemoteURL())
	require.Equal(t, RemoteStarting, instance.RemoteState())
	instance.remoteSince = time.Now().Add(-time.Minute)
	require.Equal(t, RemoteFailed, instance.RemoteState(), "asked for, but off well after the start")

	// Without an entry, as when the registry cannot be read for a moment, the state stays.
	register(t, entry, `{"pid":50251,"sessionId":"conv-1","bridgeSessionId":"session_01ABC"}`)
	instance.SetClaudeProcess(instance.ComputeClaudeProcess())
	instance.SetClaudeProcess(nil)
	require.Equal(t, RemoteOn, instance.RemoteState())

	instance.SetStatus(Paused)
	require.Equal(t, RemoteStarting, instance.RemoteState(), "connects again once resumed")
	require.Empty(t, instance.RemoteURL())
}

// Other agents have no Remote Control.
func TestRemoteControlNeedsClaude(t *testing.T) {
	instance, err := NewInstance(InstanceOptions{Title: "codex", Path: t.TempDir(), Program: "codex", RemoteControl: true})
	require.NoError(t, err)
	require.Equal(t, RemoteOff, instance.RemoteState())
	require.ErrorContains(t, instance.EnableRemoteControl(), "needs Claude Code")
}

// Turning Remote Control on in a running session sends /remote-control once Claude Code is
// idle with an empty prompt, and only once.
func TestEnableRemoteControlWaitsForEmptyPrompt(t *testing.T) {
	instance, fake, entry := newRemoteInstance(t, "later")
	require.NoError(t, instance.EnableRemoteControl())
	require.True(t, instance.RemoteControl)
	require.Contains(t, instance.launchProgram(), "--remote-control", "a restart keeps it on")
	require.Equal(t, RemoteStarting, instance.RemoteState())

	fake.pane = "────\n\x1b[39m❯ draft I am typing\x1b[7m \x1b[27m\n────\n"
	sent, err := instance.SyncRemoteControl()
	require.NoError(t, err)
	require.False(t, sent, "the user is typing")
	instance.remoteSince = time.Now().Add(-time.Hour)
	require.Equal(t, RemoteStarting, instance.RemoteState(), "waiting to be sent is not a failure")

	fake.pane = "────\n❯ \n────\n"
	sent, err = instance.SyncRemoteControl()
	require.NoError(t, err)
	require.True(t, sent)
	require.Equal(t, "/remote-control\r", fake.typed())

	sent, err = instance.SyncRemoteControl()
	require.NoError(t, err)
	require.False(t, sent, "sent once")

	// Asked for again while already connected, the command would open Claude Code's panel.
	register(t, entry, `{"pid":50251,"sessionId":"conv-1","bridgeSessionId":"session_01ABC"}`)
	instance.SetClaudeProcess(instance.ComputeClaudeProcess())
	require.NoError(t, instance.EnableRemoteControl())
	sent, err = instance.SyncRemoteControl()
	require.NoError(t, err)
	require.False(t, sent)
}

// Disconnecting opens Claude Code's Remote Control panel, and keeps later starts from turning
// Remote Control on.
func TestDisableRemoteControl(t *testing.T) {
	instance, fake, entry := newRemoteInstance(t, "off")
	instance.RemoteControl = true
	register(t, entry, `{"pid":50251,"sessionId":"conv-1","bridgeSessionId":"session_01ABC"}`)
	instance.SetClaudeProcess(instance.ComputeClaudeProcess())

	fake.pane = "────\n❯ half a sentence\n────\n"
	_, err := instance.DisableRemoteControl()
	require.ErrorContains(t, err, "busy or has a draft")
	require.False(t, instance.RemoteControl)

	fake.pane = "────\n❯ \n────\n"
	opened, err := instance.DisableRemoteControl()
	require.NoError(t, err)
	require.True(t, opened)
	require.Equal(t, "/remote-control\r", fake.typed())
	require.NotContains(t, instance.launchProgram(), "--remote-control")
}

// A Claude instance whose tmux session ended continues its conversation when resumed, which
// also brings back its Remote Control session on claude.ai. Claude Code only saves a
// conversation once it has a message; before that, a restart starts afresh.
func TestResumeContinuesConversation(t *testing.T) {
	instance, fake, entry := newRemoteInstance(t, "continue")
	instance.RemoteControl = true
	register(t, entry, `{"pid":50251,"sessionId":"conv-1","bridgeSessionId":"session_01ABC"}`)
	instance.SetClaudeProcess(instance.ComputeClaudeProcess())

	fake.alive = false
	require.NoError(t, instance.MarkEnded())
	require.Equal(t, Paused, instance.Status)
	require.NoError(t, instance.Resume())
	require.Len(t, fake.sessions, 2)
	require.NotContains(t, fake.sessions[1], "--resume", "no saved conversation yet")

	saveTranscript(t, "conv-1")
	fake.alive = false
	require.NoError(t, instance.MarkEnded())
	require.NoError(t, instance.Resume())
	require.Len(t, fake.sessions, 3)
	require.Contains(t, fake.sessions[2], "--remote-control")
	require.True(t, strings.HasSuffix(fake.sessions[2], "--resume 'conv-1'"), fake.sessions[2])
	require.Equal(t, Running, instance.Status)

	// The conversation cannot be continued and Claude Code exits at once: the next restart
	// starts a new conversation.
	fake.alive = false
	require.ErrorContains(t, instance.MarkEnded(), "exited right after continuing its conversation")
	require.NoError(t, instance.Resume())
	require.NotContains(t, fake.sessions[3], "--resume")
}

// An instance ending long after it was resumed did continue its conversation.
func TestMarkEndedLongAfterResume(t *testing.T) {
	instance, _, _ := newRemoteInstance(t, "late")
	instance.claudeSessionID = "conv-1"
	instance.resumedAt = time.Now().Add(-time.Hour)
	require.NoError(t, instance.MarkEnded())
	require.Equal(t, "conv-1", instance.claudeSessionID)
}

// Remote Control and the conversation to continue survive a restart of claude-squad.
func TestRemoteControlRoundTripsThroughStorage(t *testing.T) {
	instance, _, entry := newRemoteInstance(t, "stored")
	instance.RemoteControl = true
	register(t, entry, `{"pid":50251,"sessionId":"conv-1","bridgeSessionId":"session_01ABC"}`)
	instance.SetClaudeProcess(instance.ComputeClaudeProcess())

	data := instance.ToInstanceData()
	require.True(t, data.RemoteControl)
	require.Equal(t, "conv-1", data.ClaudeSessionID)

	data.Status = Paused
	restored, err := FromInstanceData(data)
	require.NoError(t, err)
	require.True(t, restored.RemoteControl)
	require.Equal(t, "conv-1", restored.claudeSessionID)
	saveTranscript(t, "conv-1")
	args, resumed := restored.restartArgs()
	require.True(t, resumed)
	require.Equal(t, "--resume 'conv-1'", args)
	require.NoError(t, instance.Kill())
}

func TestRemoteURL(t *testing.T) {
	require.Equal(t, "https://claude.ai/code/session_01PGK9UUiG9Uvb8nTr2TH9bP", claudestatus.RemoteURL("session_01PGK9UUiG9Uvb8nTr2TH9bP"))
}
