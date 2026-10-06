package app

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/ui"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeServerTmux stands in for tmux running the server's session.
type fakeServerTmux struct {
	exists bool
	dead   bool
	pane   string
	ran    []string
}

func (f *fakeServerTmux) exec() cmd_test.MockCmdExec {
	return cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			f.ran = append(f.ran, strings.Join(cmd.Args[1:], " "))
			switch cmd.Args[1] {
			case "has-session":
				if !f.exists {
					return fmt.Errorf("can't find session")
				}
			case "new-session":
				f.exists = true
			case "respawn-pane":
				f.dead = false
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			switch {
			case cmd.Args[1] == "capture-pane":
				return []byte(f.pane), nil
			case strings.Contains(cmd.String(), "pane_dead") && f.dead:
				return []byte("1\n"), nil
			}
			return []byte("0\n"), nil
		},
	}
}

func newTestServer(f *fakeServerTmux) *remoteServer {
	s := newRemoteServer("/home/me/Projects/FOYS/foys-all", "/home/me/.local/bin/claude --model opus")
	s.exec = f.exec()
	return s
}

// lookNow checks the server now, whenever it last did.
func (s *remoteServer) lookNow() (ui.ServerState, string) {
	s.checkedAt = time.Time{}
	return s.check()
}

func TestRemoteServerStartsInItsOwnTmuxSession(t *testing.T) {
	f := &fakeServerTmux{}
	s := newTestServer(f)
	require.Equal(t, "claudesquad-rc_foys-all_", s.name[:len("claudesquad-rc_foys-all_")])
	require.Len(t, s.name, len("claudesquad-rc_foys-all_")+6)

	state, _ := s.lookNow()
	require.Equal(t, ui.ServerStarting, state)
	require.Len(t, f.ran, 2)
	start := f.ran[1]
	require.True(t, strings.HasPrefix(start, "new-session -d -s "+s.name+" -c /home/me/Projects/FOYS/foys-all ; set-option -w -t ="+s.name+": remain-on-exit on ; respawn-pane -k"), start)
	require.True(t, strings.HasSuffix(start, "'/home/me/.local/bin/claude' remote-control --spawn same-dir --no-create-session-in-dir"),
		"server mode refuses the program's flags: %s", start)

	f.pane = "Remote Control is launching in spawn mode\n·✔︎· Ready · foys-all\n    Capacity: 0/32\n"
	state, _ = s.lookNow()
	require.Equal(t, ui.ServerReady, state)

	f.pane = "·✔︎· Connected · repo · master\n"
	state, _ = s.lookNow()
	require.Equal(t, ui.ServerReady, state)
	require.Len(t, f.ran, 4, "no second start while it runs")

	// Checks in between report the last result without looking.
	state, _ = s.check()
	require.Equal(t, ui.ServerReady, state)
	require.Len(t, f.ran, 4)
}

func TestRemoteServerAsksQuestion(t *testing.T) {
	f := &fakeServerTmux{exists: true, pane: "Quick safety check\nTrust /home/me/Projects/FOYS/foys-all? [y/N]\n"}
	state, problem := newTestServer(f).lookNow()
	require.Equal(t, ui.ServerNeedsInput, state)
	require.Equal(t, "Trust /home/me/Projects/FOYS/foys-all? [y/N]", problem)
}

// A server that stopped keeps its pane, so cs can say why, and starts again in it after a
// while.
func TestRemoteServerRestartsAfterStopping(t *testing.T) {
	f := &fakeServerTmux{exists: true, dead: true, pane: "Remote Control requires a claude.ai subscription\n\nPane is dead (status 1)\n"}
	s := newTestServer(f)
	s.startedAt = time.Now()

	state, problem := s.lookNow()
	require.Equal(t, ui.ServerDown, state)
	require.Equal(t, "Remote Control requires a claude.ai subscription", problem)
	require.NotContains(t, strings.Join(f.ran, "\n"), "respawn-pane", "not again right away")

	s.startedAt = time.Now().Add(-time.Minute)
	state, _ = s.lookNow()
	require.Equal(t, ui.ServerDown, state)
	require.Contains(t, f.ran[len(f.ran)-1], "respawn-pane -k -t ="+s.name+": -c /home/me/Projects/FOYS/foys-all")
	require.False(t, f.dead)
}

func TestNoServerForOtherPrograms(t *testing.T) {
	require.Nil(t, newRemoteServer("/src", "aider --model x"))
}
