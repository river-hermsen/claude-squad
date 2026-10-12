package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newRegisteredInstance starts an in-place Claude instance whose pane runs process 50251, and
// returns where Claude Code's registry entry of that process goes.
func newRegisteredInstance(t *testing.T, title string) (*Instance, *fakeTmux, string) {
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

// The instance learns the conversation in its pane from Claude Code's registry.
func TestSetClaudeProcessFollowsRegistry(t *testing.T) {
	instance, _, entry := newRegisteredInstance(t, "registry")

	register(t, entry, `{"pid":50251,"sessionId":"conv-1"}`)
	require.True(t, instance.SetClaudeProcess(instance.ComputeClaudeProcess()), "a new conversation is worth saving")
	require.Equal(t, "conv-1", instance.claudeSessionID)
	require.False(t, instance.SetClaudeProcess(instance.ComputeClaudeProcess()), "same conversation")

	// Without an entry, as when the registry cannot be read for a moment, it stays.
	require.False(t, instance.SetClaudeProcess(nil))
	require.Equal(t, "conv-1", instance.claudeSessionID)
}

// A Claude instance whose tmux session ended continues its conversation when resumed. Claude
// Code only saves a conversation once it has a message; before that, a restart starts afresh.
func TestResumeContinuesConversation(t *testing.T) {
	instance, fake, entry := newRegisteredInstance(t, "continue")
	register(t, entry, `{"pid":50251,"sessionId":"conv-1"}`)
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
	instance, _, _ := newRegisteredInstance(t, "late")
	instance.claudeSessionID = "conv-1"
	instance.resumedAt = time.Now().Add(-time.Hour)
	require.NoError(t, instance.MarkEnded())
	require.Equal(t, "conv-1", instance.claudeSessionID)
}

// The conversation to continue survives a restart of claude-squad.
func TestConversationRoundTripsThroughStorage(t *testing.T) {
	instance, _, entry := newRegisteredInstance(t, "stored")
	register(t, entry, `{"pid":50251,"sessionId":"conv-1"}`)
	instance.SetClaudeProcess(instance.ComputeClaudeProcess())

	data := instance.ToInstanceData()
	require.Equal(t, "conv-1", data.ClaudeSessionID)

	data.Status = Paused
	restored, err := FromInstanceData(data)
	require.NoError(t, err)
	require.Equal(t, "conv-1", restored.claudeSessionID)
	saveTranscript(t, "conv-1")
	args, resumed := restored.restartArgs()
	require.True(t, resumed)
	require.Equal(t, "--resume 'conv-1'", args)
	require.NoError(t, instance.Kill())
}
