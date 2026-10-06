package app

import (
	"claude-squad/cmd"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/ui"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// remoteServer keeps a Remote Control server, `claude remote-control`, running for the
// directory cs runs in, in a tmux session of its own that outlives cs. The Claude app then
// can always start a conversation on this machine, and that conversation starts cs sessions
// with `cs new`; see skill.go. The server starts no session of its own, and its sessions all
// work in the directory itself.
//
// Its methods run on the metadata tick's goroutine, one call at a time.
type remoteServer struct {
	// name is the server's tmux session.
	name   string
	dir    string
	claude string
	exec   cmd.Executor

	// startedAt is when cs last started the server; it waits restartDelay before doing so
	// again.
	startedAt time.Time
	// checkedAt and state are the last check's time and result; see check.
	checkedAt time.Time
	state     ui.ServerState
	problem   string
}

const (
	serverPrefix = "claudesquad-rc_"
	// restartDelay is how long cs waits before starting a server that stopped again.
	restartDelay = 30 * time.Second
	// serverCheckInterval is how often check looks at the server.
	serverCheckInterval = 2 * time.Second
)

var (
	// serverReady matches the server's status line once it is connected, such as
	// "·✔︎· Ready · foys-all" or "·✔︎· Connected · repo · master".
	serverReady = regexp.MustCompile(`·\s*(Ready|Connected)\b`)
	// serverQuestion matches the questions the server asks in its terminal before starting:
	// whether to trust the folder, and whether to enable Remote Control at all.
	serverQuestion = regexp.MustCompile(`\[y/N\]|\(y/n\)|Choose \[`)
)

// newRemoteServer returns the server for dir, run by program's claude binary, or nil if
// program is not Claude Code.
func newRemoteServer(dir string, program string) *remoteServer {
	if !session.IsClaudeProgram(program) {
		return nil
	}
	return &remoteServer{
		name:   serverSessionName(dir),
		dir:    dir,
		claude: strings.Fields(program)[0],
		exec:   cmd.MakeExecutor(),
	}
}

// serverSessionName names the tmux session of dir's server after the directory, with a hash
// of its path so two directories of the same name get a server each.
func serverSessionName(dir string) string {
	base := strings.NewReplacer(".", "_", ":", "_", " ", "").Replace(filepath.Base(dir))
	return fmt.Sprintf("%s%s_%x", serverPrefix, base, sha256.Sum256([]byte(dir)))[:len(serverPrefix)+len(base)+7]
}

// command is what the server's tmux pane runs. Server mode refuses flags it cannot pass on to
// its sessions, such as --settings, so it gets none of the instance's.
func (s *remoteServer) command() string {
	return shellQuote(s.claude) + " remote-control --spawn same-dir --no-create-session-in-dir"
}

// check starts the server if it is not running, starts it again some time after it stopped,
// and reports its state, with what it printed last when it needs an answer or stopped. It
// looks at most every serverCheckInterval and otherwise reports the last result.
func (s *remoteServer) check() (ui.ServerState, string) {
	if time.Since(s.checkedAt) < serverCheckInterval {
		return s.state, s.problem
	}
	s.checkedAt = time.Now()
	s.state, s.problem = s.look()
	return s.state, s.problem
}

func (s *remoteServer) look() (ui.ServerState, string) {
	target := "=" + s.name + ":"
	if s.exec.Run(exec.Command("tmux", "has-session", "-t="+s.name)) != nil {
		if err := s.start(); err != nil {
			return ui.ServerDown, err.Error()
		}
		return ui.ServerStarting, ""
	}
	out, err := s.exec.Output(exec.Command("tmux", "capture-pane", "-p", "-J", "-t", target))
	if err != nil {
		return ui.ServerStarting, ""
	}
	content := string(out)
	dead, _ := s.exec.Output(exec.Command("tmux", "display-message", "-p", "-t", target, "#{pane_dead}"))
	if strings.TrimSpace(string(dead)) == "1" {
		problem := lastLine(content)
		if time.Since(s.startedAt) > restartDelay {
			log.WarningLog.Printf("Remote Control server for %s stopped (%s); starting it again", s.dir, problem)
			if err := s.respawn(); err != nil {
				return ui.ServerDown, err.Error()
			}
		}
		return ui.ServerDown, problem
	}
	switch {
	case serverReady.MatchString(content):
		return ui.ServerReady, ""
	case serverQuestion.MatchString(content):
		return ui.ServerNeedsInput, lastLine(content)
	default:
		return ui.ServerStarting, ""
	}
}

// start creates the server's tmux session. The pane stays when the server exits, so check can
// show why and start it again in place. It is set up first with a shell, so even a server that
// exits at once leaves its pane.
func (s *remoteServer) start() error {
	s.startedAt = time.Now()
	target := "=" + s.name + ":"
	err := s.exec.Run(exec.Command("tmux",
		"new-session", "-d", "-s", s.name, "-c", s.dir, ";",
		"set-option", "-w", "-t", target, "remain-on-exit", "on", ";",
		"respawn-pane", "-k", "-t", target, "-c", s.dir, s.command()))
	if err != nil {
		return fmt.Errorf("could not start the Remote Control server: %w", err)
	}
	log.InfoLog.Printf("started Remote Control server for %s in tmux session %s", s.dir, s.name)
	return nil
}

func (s *remoteServer) respawn() error {
	s.startedAt = time.Now()
	err := s.exec.Run(exec.Command("tmux", "respawn-pane", "-k", "-t", "="+s.name+":", "-c", s.dir, s.command()))
	if err != nil {
		return fmt.Errorf("could not restart the Remote Control server: %w", err)
	}
	return nil
}

// lastLine returns the last line of content with text on it.
func lastLine(content string) string {
	lines := strings.Split(strings.TrimRight(content, " \n"), "\n")
	for k := len(lines) - 1; k >= 0; k-- {
		if line := strings.TrimSpace(lines[k]); line != "" && !strings.HasPrefix(line, "Pane is dead") {
			return line
		}
	}
	return ""
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// setServerState shows the Remote Control server's state in the list, and says once why, when
// it needs an answer in its terminal or stopped.
func (m *home) setServerState(state ui.ServerState, problem string) tea.Cmd {
	prev := m.serverState
	m.serverState = state
	m.list.SetServer(state)
	if state == prev {
		return nil
	}
	switch state {
	case ui.ServerNeedsInput:
		return m.handleError(fmt.Errorf("the Remote Control server asks %q: answer it with tmux attach -t %s", problem, m.server.name))
	case ui.ServerDown:
		return m.handleError(fmt.Errorf("the Remote Control server stopped (%s); cs starts it again", problem))
	}
	return nil
}
