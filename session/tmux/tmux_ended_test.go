package tmux

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/log"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A session whose program exited is gone: capturing it fails and tmux no longer has it.
func TestEndedAfterProgramExits(t *testing.T) {
	alive := true
	exec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			if strings.Contains(cmd.String(), "has-session") && !alive {
				return fmt.Errorf("can't find session")
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			if !alive {
				return nil, fmt.Errorf("exit status 1")
			}
			return []byte("screen"), nil
		},
	}
	log.Initialize(false)
	session := NewTmuxSessionWithDeps("ended", "claude", nil, exec)
	session.monitor = newStatusMonitor()

	session.HasUpdated()
	require.False(t, session.Ended())

	alive = false
	session.HasUpdated()
	require.True(t, session.Ended())
}
