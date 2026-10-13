package session

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// promptPane is Claude Code's pane with line as its prompt line.
func promptPane(line string) string {
	const rule = "────────────"
	return "⏺ done\n" + rule + "\n" + line + "\n" + rule + "\n  Opus 4.7 · status\n"
}

// A session running an older Claude Code than the installed one offers an update.
func TestUpdateAvailable(t *testing.T) {
	instance, fake, entry := newRegisteredInstance(t, "update")
	register(t, entry, `{"pid":50251,"sessionId":"conv-1","version":"2.1.296","status":"idle"}`)
	instance.SetClaudeProcess(instance.ComputeClaudeProcess())
	require.False(t, instance.UpdateAvailable(), "installed version unknown")
	instance.SetInstalledClaudeVersion("2.1.296")
	require.False(t, instance.UpdateAvailable())
	instance.SetInstalledClaudeVersion("2.1.297")
	require.True(t, instance.UpdateAvailable())

	// A session that ended runs no Claude Code until it is resumed.
	fake.alive = false
	require.NoError(t, instance.MarkEnded())
	require.False(t, instance.UpdateAvailable())
}

// The confirmation of a restart warns about what it cuts short.
func TestRestartWarnings(t *testing.T) {
	instance, fake, entry := newRegisteredInstance(t, "warn")
	fake.pane = promptPane("❯ ")
	for _, tc := range []struct {
		status string
		want   string
	}{
		{"idle", ""},
		{"busy", "Claude is working"},
		{"shell", "Shell commands"},
	} {
		register(t, entry, `{"pid":50251,"sessionId":"conv-1","status":"`+tc.status+`"}`)
		instance.SetClaudeProcess(instance.ComputeClaudeProcess())
		warnings := instance.RestartWarnings()
		if tc.want == "" {
			require.Empty(t, warnings, tc.status)
		} else {
			require.Len(t, warnings, 1, tc.status)
			require.Contains(t, warnings[0], tc.want)
		}
	}

	register(t, entry, `{"pid":50251,"sessionId":"conv-1","status":"idle"}`)
	instance.SetClaudeProcess(instance.ComputeClaudeProcess())
	fake.pane = promptPane("❯ half a thought")
	warnings := instance.RestartWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "prompt is not empty")
}

// Restarting a running session runs the installed Claude Code on the same conversation.
func TestRestartContinuesConversation(t *testing.T) {
	instance, fake, entry := newRegisteredInstance(t, "restart")
	register(t, entry, `{"pid":50251,"sessionId":"conv-1","version":"2.1.296","status":"idle"}`)
	saveTranscript(t, "conv-1")
	instance.SetInstalledClaudeVersion("2.1.297")
	instance.SetClaudeProcess(instance.ComputeClaudeProcess())
	require.True(t, instance.UpdateAvailable())

	_, err := instance.StopForRestart()
	require.NoError(t, err)
	require.False(t, fake.alive, "the tmux session is closed")
	require.True(t, instance.Restarting())
	require.Equal(t, Loading, instance.Status)
	require.False(t, instance.UpdateAvailable(), "no process runs")
	_, err = instance.StopForRestart()
	require.Error(t, err, "already stopped")

	require.NoError(t, instance.FinishRestart())
	require.False(t, instance.Restarting())
	require.Equal(t, Running, instance.Status)
	require.Len(t, fake.sessions, 2)
	require.True(t, strings.HasSuffix(fake.sessions[1], "--resume 'conv-1'"), fake.sessions[1])
	require.NoError(t, instance.FinishRestart(), "nothing to finish")
	require.Len(t, fake.sessions, 2)
}

// Only a running Claude session can be restarted.
func TestStopForRestartNeedsRunningClaude(t *testing.T) {
	instance, fake, _ := newRegisteredInstance(t, "paused")
	fake.alive = false
	require.NoError(t, instance.MarkEnded())
	_, err := instance.StopForRestart()
	require.ErrorContains(t, err, "not running")

	other, _ := newInPlaceInstance(t, "other")
	other.claudeDir = ""
	_, err = other.StopForRestart()
	require.ErrorContains(t, err, "only Claude Code")
}

func TestWaitForExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep")
	}
	cmd := exec.Command("sleep", "0.2")
	require.NoError(t, cmd.Start())
	// Reap it, as tmux reaps Claude Code; a zombie still exists.
	go func() { _ = cmd.Wait() }()
	start := time.Now()
	waitForExit(cmd.Process.Pid, 5*time.Second)
	require.Less(t, time.Since(start), 3*time.Second)
}
