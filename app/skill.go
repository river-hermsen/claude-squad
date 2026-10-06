package app

import (
	"claude-squad/session/claudestatus"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// skillMarker marks the claude-squad skill as written by cs, which may rewrite it.
const skillMarker = "<!-- Written by claude-squad (cs), which rewrites this file. -->"

// skillContent is the claude-squad skill, which tells Claude Code how to start a cs session
// with csPath, the cs binary.
func skillContent(csPath string) string {
	return fmt.Sprintf(`---
name: claude-squad
description: Start a new claude-squad (cs) session on this machine - a full Claude Code session, isolated in its own git worktree, that shows in the cs list here and can be continued from the Claude app. Use when the user asks to start, spawn, launch or open a new session, agent, worker or "cs session" for a task, for example from their phone.
---

%s

# Start a claude-squad session

Run this from the directory the session should work in, usually the current one:

    %s new --title "<short title>" --prompt "<the task, in the user's words>"

- `+"`--title`"+`: a few words, at most 32 characters. cs adds a number if the title is taken.
- `+"`--prompt`"+`: what the new session should do. Leave it out to start an idle session.
- `+"`--path <dir>`"+`: start it in another directory, such as a single repository.

The command takes up to two minutes: it waits until the session is ready, gives it the task,
and prints the session's Remote Control link. Reply with that link, so the user can tap it
and continue in that session. The session also shows in cs on this machine.

Do not do the task yourself: the new session does it. If the command fails, tell the user
what it printed.
`, skillMarker, csPath)
}

// installSkill writes the claude-squad skill into the user's Claude Code skills, so Claude
// knows how to start cs sessions, as from a conversation the Claude app starts through the
// Remote Control server. A skill of that name that cs did not write is left alone.
func installSkill(csPath string) error {
	dir := claudestatus.ConfigDir()
	if dir == "" {
		return nil
	}
	path := filepath.Join(dir, "skills", "claude-squad", "SKILL.md")
	content := skillContent(csPath)
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) == content || !strings.Contains(string(existing), skillMarker) {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
