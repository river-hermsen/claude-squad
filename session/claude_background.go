package session

import (
	"claude-squad/log"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// backgroundSession is an entry of `claude agents --json`.
type backgroundSession struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Cwd       string `json:"cwd"`
}

// belongsTo reports whether the background session continues the conversation of a Claude
// instance: the conversation sessionID, which Claude Code shortens in background ids, or if
// that is unknown, the session named name, as cs names its sessions, in cwd.
func (b backgroundSession) belongsTo(sessionID, name, cwd string) bool {
	if b.Kind != "background" {
		return false
	}
	id := b.SessionID
	if id == "" {
		id = b.ID
	}
	if sessionID != "" {
		return id != "" && strings.HasPrefix(sessionID, id)
	}
	// Without the conversation's id, fall back to its name, which cs gave it.
	return name != "" && b.Name == name && samePath(b.Cwd, cwd)
}

// samePath reports whether a and b name the same directory, through symlinks such as macOS's
// /var -> /private/var.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// stopBackgroundConversation stops the background session, if any, that continues a killed
// Claude instance's conversation. Claude Code can move a conversation to the background,
// where it outlives the tmux session, keeps running and makes resuming it elsewhere fail.
// Claude may still be moving it when the tmux session closes, so it looks a few times, in
// the background. It is a variable for tests.
var stopBackgroundConversation = func(program, sessionID, name, cwd string) {
	fields := strings.Fields(program)
	if len(fields) == 0 {
		return
	}
	claude := fields[0]
	go func() {
		for _, wait := range []time.Duration{0, 2 * time.Second, 5 * time.Second} {
			time.Sleep(wait)
			out, err := exec.Command(claude, "agents", "--json").Output()
			if err != nil {
				log.WarningLog.Printf("could not list Claude background sessions: %v", err)
				return
			}
			var sessions []backgroundSession
			if err := json.Unmarshal(out, &sessions); err != nil {
				log.WarningLog.Printf("could not parse Claude background sessions: %v", err)
				return
			}
			for _, s := range sessions {
				if !s.belongsTo(sessionID, name, cwd) {
					continue
				}
				id := s.ID
				if id == "" {
					id = s.SessionID
				}
				if out, err := exec.Command(claude, "stop", id).CombinedOutput(); err != nil {
					log.WarningLog.Printf("could not stop Claude background session %s: %v: %s", id, err, out)
				} else {
					log.InfoLog.Printf("stopped Claude background session %s (%s)", id, s.Name)
				}
				return
			}
		}
	}()
}
