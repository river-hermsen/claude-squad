// Package claudestatus reports the model, effort and context use of the Claude Code sessions
// claude-squad runs. Claude Code hands these to its status line command, so every Claude
// session gets settings that make `cs statusline` that command. It saves them where the TUI
// reads them, then runs the user's own status line, so what Claude shows does not change.
package claudestatus

import (
	"bytes"
	"claude-squad/config"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	settingsFileName = "settings.json"
	statusFileName   = "status.json"
)

// Info is what Claude Code last reported about a session.
type Info struct {
	// Model is the model's display name, such as "Opus 4.7".
	Model string `json:"model"`
	// Effort is the reasoning effort level, or "" for a model without one.
	Effort string `json:"effort,omitempty"`
	// ContextUsed is the number of tokens in the context window, ContextSize its size.
	ContextUsed int `json:"context_used"`
	ContextSize int `json:"context_size"`
	// SessionID is Claude Code's id for the conversation, which `--resume` takes.
	SessionID string `json:"session_id,omitempty"`
	// SessionName is the conversation's name: the one given with --name or /rename, or
	// one Claude Code made up from its first prompt.
	SessionName string `json:"session_name,omitempty"`
	// UpdatedAt is when Claude Code reported this.
	UpdatedAt time.Time `json:"updated_at"`
}

// Limits are the account's usage limits: a rolling 5-hour window and a weekly one. They are
// the same for every session, so RunStatusLine keeps the latest in one file; see ReadLimits.
type Limits struct {
	FiveHour Limit `json:"five_hour"`
	Week     Limit `json:"week"`
	// UpdatedAt is when Claude Code reported them.
	UpdatedAt time.Time `json:"updated_at"`
}

// Limit is how much of one usage limit is used, and when it resets.
type Limit struct {
	UsedPercent float64   `json:"used_percent"`
	ResetsAt    time.Time `json:"resets_at"`
}

// statusLineInput is the part of Claude Code's status line input used here.
type statusLineInput struct {
	Model struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Effort *struct {
		Level string `json:"level"`
	} `json:"effort"`
	ContextWindow struct {
		TotalInputTokens  float64 `json:"total_input_tokens"`
		ContextWindowSize float64 `json:"context_window_size"`
	} `json:"context_window"`
	RateLimits *struct {
		FiveHour *rateLimit `json:"five_hour"`
		SevenDay *rateLimit `json:"seven_day"`
	} `json:"rate_limits"`
	SessionID   string `json:"session_id"`
	SessionName string `json:"session_name"`
	Cwd         string `json:"cwd"`
	Workspace   struct {
		ProjectDir string `json:"project_dir"`
	} `json:"workspace"`
}

type rateLimit struct {
	UsedPercentage float64 `json:"used_percentage"`
	// ResetsAt is a Unix time in seconds.
	ResetsAt float64 `json:"resets_at"`
}

func (r *rateLimit) limit() Limit {
	if r == nil {
		return Limit{}
	}
	limit := Limit{UsedPercent: r.UsedPercentage}
	if r.ResetsAt > 0 {
		limit.ResetsAt = time.Unix(int64(r.ResetsAt), 0)
	}
	return limit
}

func (in statusLineInput) info(now time.Time) Info {
	info := Info{
		Model:       in.Model.DisplayName,
		ContextUsed: int(in.ContextWindow.TotalInputTokens),
		ContextSize: int(in.ContextWindow.ContextWindowSize),
		SessionID:   in.SessionID,
		SessionName: in.SessionName,
		UpdatedAt:   now,
	}
	if info.Model == "" {
		info.Model = in.Model.ID
	}
	if in.Effort != nil {
		info.Effort = in.Effort.Level
	}
	return info
}

// limits returns the usage limits in the input, or nil before Claude Code's first request.
func (in statusLineInput) limits(now time.Time) *Limits {
	if in.RateLimits == nil || (in.RateLimits.FiveHour == nil && in.RateLimits.SevenDay == nil) {
		return nil
	}
	return &Limits{FiveHour: in.RateLimits.FiveHour.limit(), Week: in.RateLimits.SevenDay.limit(), UpdatedAt: now}
}

// LimitsPath returns the file that holds the latest usage limits any session reported.
func LimitsPath() (string, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "limits.json"), nil
}

// ReadLimits returns the usage limits saved at path, or nil if none were saved yet.
func ReadLimits(path string) *Limits {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var limits Limits
	if err := json.Unmarshal(data, &limits); err != nil {
		return nil
	}
	return &limits
}

func sessionsDir() (string, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "sessions"), nil
}

// NewDir returns a new directory for the settings and status of the session titled title.
// The name is unique, so it stays the session's own after a rename.
func NewDir(title string) (string, error) {
	return Dir(fmt.Sprintf("%s_%x", title, time.Now().UnixNano()))
}

// Dir returns the directory named name, escaped, for a session's settings and status.
func Dir(name string) (string, error) {
	parent, err := sessionsDir()
	if err != nil {
		return "", err
	}
	// Escaping dots too keeps "." and ".." from naming the parent.
	return filepath.Join(parent, strings.ReplaceAll(url.PathEscape(name), ".", "%2E")), nil
}

// WriteSettings writes the Claude Code settings that Args loads into dir: `cs statusline`
// (csPath is the claude-squad binary) as the status line, and hooks, if not nil.
func WriteSettings(dir string, csPath string, hooks map[string]any) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	limitsPath, err := LimitsPath()
	if err != nil {
		return err
	}
	settings := map[string]any{
		"statusLine": map[string]any{
			"type": "command",
			"command": fmt.Sprintf("%s statusline --out %s --limits %s",
				shellQuote(csPath), shellQuote(filepath.Join(dir, statusFileName)), shellQuote(limitsPath)),
		},
	}
	if hooks != nil {
		settings["hooks"] = hooks
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, settingsFileName), data)
}

// Args returns the Claude Code arguments that load the settings WriteSettings wrote to dir.
// Settings passed this way merge with the user's own hooks, but replace their status line.
func Args(dir string) string {
	return "--settings " + shellQuote(filepath.Join(dir, settingsFileName))
}

// Read returns what Claude Code last reported to the status line installed in dir, from any
// conversation, or nil if it has reported nothing yet.
func Read(dir string) *Info {
	return readInfo(filepath.Join(dir, statusFileName))
}

// ReadSession returns what Claude Code last reported to the status line installed in dir
// about the conversation sessionID, or nil if it has reported nothing about it yet.
func ReadSession(dir string, sessionID string) *Info {
	return readInfo(sessionStatusPath(dir, sessionID))
}

func sessionStatusPath(dir string, sessionID string) string {
	return filepath.Join(dir, "status", url.PathEscape(sessionID)+".json")
}

func readInfo(path string) *Info {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil
	}
	return &info
}

// Process is the entry of a running Claude Code process in Claude Code's registry of running
// sessions, ~/.claude/sessions/<pid>.json.
type Process struct {
	// SessionID is the id of the conversation the process runs, which `--resume` takes.
	SessionID string `json:"sessionId"`
	// RemoteSessionID is the claude.ai session that Remote Control connects the conversation
	// to, or "" while Remote Control is off. Claude Code sets it to null on disconnect.
	RemoteSessionID string `json:"bridgeSessionId"`
}

// ReadProcess returns the registry entry of the Claude Code process pid, or nil if there is
// none, as when pid is not a Claude Code process.
func ReadProcess(pid int) *Process {
	dir := claudeConfigDir()
	if dir == "" || pid <= 0 {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "sessions", strconv.Itoa(pid)+".json"))
	if err != nil {
		return nil
	}
	var entry Process
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	return &entry
}

// SessionIDForPID returns the id of the conversation that the Claude Code process pid runs,
// from Claude Code's registry of running sessions, or "" if it cannot tell.
func SessionIDForPID(pid int) string {
	if p := ReadProcess(pid); p != nil {
		return p.SessionID
	}
	return ""
}

// RemoteURL returns the address of the Remote Control session remoteSessionID, where
// claude.ai/code and the Claude app continue it.
func RemoteURL(remoteSessionID string) string {
	return "https://claude.ai/code/" + remoteSessionID
}

// HasTranscript reports whether Claude Code saved the conversation sessionID, which it does
// once the conversation has a message. Only then can `--resume` continue it.
func HasTranscript(sessionID string) bool {
	dir := claudeConfigDir()
	if dir == "" || sessionID == "" || strings.ContainsAny(sessionID, `/\*?[`) {
		return false
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "projects", "*", sessionID+".jsonl"))
	return len(matches) > 0
}

// Remove deletes dir, a directory Dir returned.
func Remove(dir string) error {
	parent, err := sessionsDir()
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(parent, dir); err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("refusing to remove %s: not inside %s", dir, parent)
	}
	return os.RemoveAll(dir)
}

// RemoveAll deletes the directories of every session, for `cs reset`.
func RemoveAll() error {
	parent, err := sessionsDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(parent)
}

// RunStatusLine is the `cs statusline` command. It saves the Info in Claude Code's status
// line input to out and the usage limits to limitsPath, then runs the user's own status line
// command on the same input and copies its output to stdout, which is what Claude Code shows.
func RunStatusLine(out string, limitsPath string, in io.Reader, stdout io.Writer) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("failed to read status line input: %w", err)
	}

	var errs []error
	var input statusLineInput
	if err := json.Unmarshal(data, &input); err != nil {
		errs = append(errs, fmt.Errorf("failed to parse status line input: %w", err))
	} else {
		now := time.Now()
		info := input.info(now)
		// Background sessions dispatched from the session inherit its settings, and so this
		// status line. Each conversation's report is kept apart, so the TUI can read the
		// one in its tmux pane; see ReadSession.
		if err := saveInfo(out, info); err != nil {
			errs = append(errs, fmt.Errorf("failed to save status: %w", err))
		}
		if info.SessionID != "" {
			if err := saveInfo(sessionStatusPath(filepath.Dir(out), info.SessionID), info); err != nil {
				errs = append(errs, fmt.Errorf("failed to save status: %w", err))
			}
		}
		if limits := input.limits(now); limits != nil && limitsPath != "" {
			if err := saveJSON(limitsPath, limits); err != nil {
				errs = append(errs, fmt.Errorf("failed to save usage limits: %w", err))
			}
		}
	}

	projectDir := input.Workspace.ProjectDir
	if projectDir == "" {
		projectDir = input.Cwd
	}
	if command := userStatusLine(projectDir); command != "" {
		cmd := exec.Command("sh", "-c", command)
		cmd.Stdin = bytes.NewReader(data)
		cmd.Stdout = stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			errs = append(errs, fmt.Errorf("status line command %q failed: %w", command, err))
		}
	}
	return errors.Join(errs...)
}

// saveInfo saves info at path, unless it is an empty report, as Claude Code sends while a
// session starts, on a conversation path already has numbers for.
func saveInfo(path string, info Info) error {
	if info.ContextUsed == 0 {
		if data, err := os.ReadFile(path); err == nil {
			var saved Info
			if json.Unmarshal(data, &saved) == nil && saved.SessionID == info.SessionID && saved.ContextUsed > 0 {
				return nil
			}
		}
	}
	return saveJSON(path, info)
}

func saveJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// userStatusLine returns the status line command Claude Code would run without claude-squad's
// settings: from the project's local settings, its shared settings, or the user's settings,
// whichever sets one first. It returns "" if none does.
func userStatusLine(projectDir string) string {
	var files []string
	if projectDir != "" {
		files = append(files,
			filepath.Join(projectDir, ".claude", "settings.local.json"),
			filepath.Join(projectDir, ".claude", "settings.json"))
	}
	if dir := claudeConfigDir(); dir != "" {
		files = append(files, filepath.Join(dir, "settings.json"))
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var settings struct {
			StatusLine *struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"statusLine"`
		}
		if err := json.Unmarshal(data, &settings); err != nil || settings.StatusLine == nil {
			continue
		}
		if settings.StatusLine.Type != "command" {
			return ""
		}
		return settings.StatusLine.Command
	}
	return ""
}

// claudeConfigDir returns where Claude Code keeps the user's settings.
func claudeConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
