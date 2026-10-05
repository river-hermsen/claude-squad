package claudestatus

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// statusLineJSON is trimmed from what Claude Code 2.1 passes to its status line.
const statusLineJSON = `{
  "model": {"id": "claude-opus-4-7[1m]", "display_name": "Opus 4.7 (1M context)"},
  "effort": {"level": "xhigh"},
  "context_window": {
    "total_input_tokens": 92100,
    "total_output_tokens": 49,
    "context_window_size": 1000000,
    "current_usage": {"input_tokens": 10, "output_tokens": 49, "cache_creation_input_tokens": 1553, "cache_read_input_tokens": 90537}
  },
  "rate_limits": {"five_hour": {"used_percentage": 12, "resets_at": 1791216000}, "seven_day": {"used_percentage": 40.4, "resets_at": 1791734400}},
  "session_id": "2ac8c869-dafa-4c76-b0a1-aa44b2386709",
  "session_name": "fix the map",
  "cwd": "%s",
  "workspace": {"current_dir": "%s", "project_dir": "%s"}
}`

func statusLineInputFor(projectDir string) string {
	return strings.ReplaceAll(statusLineJSON, "%s", projectDir)
}

func writeSettings(t *testing.T, path string, settings string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(settings), 0644))
}

func TestRunStatusLineSavesInfoAndRunsUserStatusLine(t *testing.T) {
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	writeSettings(t, filepath.Join(claudeDir, "settings.json"),
		`{"model": "opus", "statusLine": {"type": "command", "command": "printf 'user line: '; cat | head -c 1"}}`)

	dir := t.TempDir()
	limitsPath := filepath.Join(t.TempDir(), "limits.json")
	var stdout bytes.Buffer
	require.NoError(t, RunStatusLine(filepath.Join(dir, statusFileName), limitsPath, strings.NewReader(statusLineInputFor(t.TempDir())), &stdout))

	require.Equal(t, "user line: {", stdout.String(), "the user's status line gets the same input, and its output is shown")
	info := Read(dir)
	require.NotNil(t, info)
	require.WithinDuration(t, time.Now(), info.UpdatedAt, time.Minute)
	info.UpdatedAt = time.Time{}
	require.Equal(t, &Info{
		Model: "Opus 4.7 (1M context)", Effort: "xhigh", ContextUsed: 92100, ContextSize: 1000000,
		SessionID: "2ac8c869-dafa-4c76-b0a1-aa44b2386709", SessionName: "fix the map",
	}, info)

	limits := ReadLimits(limitsPath)
	require.NotNil(t, limits)
	require.WithinDuration(t, time.Now(), limits.UpdatedAt, time.Minute)
	limits.UpdatedAt = time.Time{}
	require.Equal(t, &Limits{
		FiveHour: Limit{UsedPercent: 12, ResetsAt: time.Unix(1791216000, 0)},
		Week:     Limit{UsedPercent: 40.4, ResetsAt: time.Unix(1791734400, 0)},
	}, limits)
}

// Background agents of a session share its settings and report the conversation with an
// empty context and no limits. That must not wipe what the session itself reported.
func TestRunStatusLineKeepsNumbersOverEmptyReport(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	out, limitsPath := filepath.Join(dir, statusFileName), filepath.Join(dir, "limits.json")
	require.NoError(t, RunStatusLine(out, limitsPath, strings.NewReader(statusLineInputFor(t.TempDir())), io.Discard))

	empty := `{"model": {"display_name": "Opus 4.7"}, "session_id": "2ac8c869-dafa-4c76-b0a1-aa44b2386709",
		"context_window": {"total_input_tokens": 0, "context_window_size": 1000000}, "rate_limits": null}`
	require.NoError(t, RunStatusLine(out, limitsPath, strings.NewReader(empty), io.Discard))
	require.Equal(t, 92100, Read(dir).ContextUsed)
	require.Equal(t, 12.0, ReadLimits(limitsPath).FiveHour.UsedPercent)

	cleared := strings.Replace(empty, "2ac8c869", "ffffffff", 1)
	require.NoError(t, RunStatusLine(out, limitsPath, strings.NewReader(cleared), io.Discard))
	require.Zero(t, Read(dir).ContextUsed, "a new conversation, as after /clear, starts empty")
}

func TestRunStatusLinePrefersProjectStatusLine(t *testing.T) {
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	writeSettings(t, filepath.Join(claudeDir, "settings.json"), `{"statusLine": {"type": "command", "command": "echo user"}}`)
	project := t.TempDir()
	writeSettings(t, filepath.Join(project, ".claude", "settings.json"), `{"statusLine": {"type": "command", "command": "echo shared"}}`)

	var stdout bytes.Buffer
	require.NoError(t, RunStatusLine(filepath.Join(t.TempDir(), statusFileName), "", strings.NewReader(statusLineInputFor(project)), &stdout))
	require.Equal(t, "shared\n", stdout.String())

	writeSettings(t, filepath.Join(project, ".claude", "settings.local.json"), `{"statusLine": {"type": "command", "command": "echo local"}}`)
	stdout.Reset()
	require.NoError(t, RunStatusLine(filepath.Join(t.TempDir(), statusFileName), "", strings.NewReader(statusLineInputFor(project)), &stdout))
	require.Equal(t, "local\n", stdout.String())
}

func TestRunStatusLineWithoutUserStatusLine(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()

	var stdout bytes.Buffer
	// Haiku has no effort level: Claude Code sends "effort": null.
	input := `{"model": {"id": "claude-haiku-4-5", "display_name": "Haiku 4.5"}, "effort": null,
		"context_window": {"total_input_tokens": 47662, "context_window_size": 200000}}`
	limitsPath := filepath.Join(dir, "limits.json")
	require.NoError(t, RunStatusLine(filepath.Join(dir, statusFileName), limitsPath, strings.NewReader(input), &stdout))
	require.Empty(t, stdout.String())
	require.Nil(t, ReadLimits(limitsPath), "no limits before the first request")
	info := Read(dir)
	require.NotNil(t, info)
	info.UpdatedAt = time.Time{}
	require.Equal(t, &Info{Model: "Haiku 4.5", ContextUsed: 47662, ContextSize: 200000}, info)
}

func TestWriteSettingsInstallsStatusLineAndHooks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := Dir("fix: a/b")
	require.NoError(t, err)
	require.Equal(t, "fix:%20a%2Fb", filepath.Base(dir), "titles become one directory")

	hooks := map[string]any{"PreToolUse": []any{}}
	require.NoError(t, WriteSettings(dir, "/usr/local/bin/cs", hooks))
	require.Equal(t, "--settings '"+filepath.Join(dir, settingsFileName)+"'", Args(dir))

	data, err := os.ReadFile(filepath.Join(dir, settingsFileName))
	require.NoError(t, err)
	var settings struct {
		StatusLine struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"statusLine"`
		Hooks map[string]any `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(data, &settings))
	require.Equal(t, "command", settings.StatusLine.Type)
	limitsPath, err := LimitsPath()
	require.NoError(t, err)
	require.Equal(t, "'/usr/local/bin/cs' statusline --out '"+filepath.Join(dir, statusFileName)+"' --limits '"+limitsPath+"'", settings.StatusLine.Command)
	require.Contains(t, settings.Hooks, "PreToolUse")

	require.Nil(t, Read(dir), "nothing reported yet")
	require.NoError(t, Remove(dir))
	require.NoDirExists(t, dir)
}

func TestRemoveStaysInsideSessionsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dot, err := Dir("..")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".claude-squad", "sessions"), filepath.Dir(dot), `".." does not escape`)

	outside := t.TempDir()
	require.Error(t, Remove(outside))
	require.DirExists(t, outside)
	require.Error(t, Remove(filepath.Join(home, ".claude-squad", "sessions")))
}

// Background sessions, such as agents dispatched from Claude's agents view, inherit the
// session's status line. Each conversation's report is kept apart, so theirs do not replace
// the session's own; their usage limits are the account's and still count.
func TestRunStatusLineKeepsConversationsApart(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	out, limitsPath := filepath.Join(dir, statusFileName), filepath.Join(dir, "limits.json")
	require.NoError(t, RunStatusLine(out, limitsPath, strings.NewReader(statusLineInputFor(t.TempDir())), io.Discard))

	agent := `{"model": {"display_name": "Opus 4.7"}, "session_id": "8329fca3-0000", "session_name": "agent",
		"context_window": {"total_input_tokens": 0, "context_window_size": 1000000}, "rate_limits": null}`
	require.NoError(t, RunStatusLine(out, limitsPath, strings.NewReader(agent), io.Discard))

	require.Equal(t, "agent", Read(dir).SessionName, "Read returns the latest report")
	session := ReadSession(dir, "2ac8c869-dafa-4c76-b0a1-aa44b2386709")
	require.NotNil(t, session)
	require.Equal(t, 92100, session.ContextUsed)
	require.Equal(t, 12.0, ReadLimits(limitsPath).FiveHour.UsedPercent)
}

func TestSessionIDForPID(t *testing.T) {
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	writeSettings(t, filepath.Join(claudeDir, "sessions", "50251.json"),
		`{"pid":50251,"sessionId":"d5fe2a96-a8c7-42b1-a133-cc035bb028fb","name":"control-plane","kind":"interactive"}`)
	require.Equal(t, "d5fe2a96-a8c7-42b1-a133-cc035bb028fb", SessionIDForPID(50251))
	require.Empty(t, SessionIDForPID(1))
}
