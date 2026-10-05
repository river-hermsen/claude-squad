package session

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/log"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"claude-squad/session/workspace"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	log.Initialize(false)
	defer log.Close()
	os.Exit(m.Run())
}

// nullPtyFactory hands back a throwaway file instead of a real PTY.
type nullPtyFactory struct {
	t     *testing.T
	calls int
}

func (p *nullPtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	p.calls++
	return os.OpenFile(filepath.Join(p.t.TempDir(), "pty"), os.O_CREATE|os.O_RDWR, 0644)
}

func (p *nullPtyFactory) Close() {}

// When the tmux server dies between runs, every session goes with it while the worktree
// and branch survive on disk. Restoring such an instance must park it as Paused so the
// user can resume it. Returning an error instead is not an option: LoadInstances aborts on
// the first failure, so one dead session would hide every other instance.
// See https://github.com/smtg-ai/claude-squad/issues/216.
func TestStartPausesInstanceWhenTmuxSessionNoLongerExists(t *testing.T) {
	ptyFactory := &nullPtyFactory{t: t}
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			if strings.Contains(cmd.String(), "has-session") {
				return fmt.Errorf("can't find session")
			}
			return nil
		},
	}

	instance, err := NewInstance(InstanceOptions{Title: "revived", Path: t.TempDir(), Program: "claude"})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("revived", "claude", ptyFactory, cmdExec))

	require.NoError(t, instance.Start(false), "a dead tmux session is recoverable, not a startup failure")
	require.Equal(t, Paused, instance.Status)
	require.True(t, instance.Started())
	require.Zero(t, ptyFactory.calls, "should not attach to a session that does not exist")
}

// The happy path is unchanged: an instance whose session survived comes back Running.
func TestStartRestoresInstanceWhenTmuxSessionSurvives(t *testing.T) {
	ptyFactory := &nullPtyFactory{t: t}
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error { return nil },
	}

	instance, err := NewInstance(InstanceOptions{Title: "alive", Path: t.TempDir(), Program: "claude"})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("alive", "claude", ptyFactory, cmdExec))

	require.NoError(t, instance.Start(false))
	require.Equal(t, Running, instance.Status)
	require.Equal(t, 1, ptyFactory.calls)
}

// fakeTmux stands in for both the PTY factory and the command executor, and tracks whether
// the session exists, so Start, Restore and Close behave the way they do against real tmux.
type fakeTmux struct {
	t        *testing.T
	alive    bool
	sessions []string // every new-session command, in order
}

func (f *fakeTmux) Start(cmd *exec.Cmd) (*os.File, error) {
	if strings.Contains(cmd.String(), "new-session") {
		f.alive = true
		f.sessions = append(f.sessions, cmd.String())
	}
	return os.OpenFile(filepath.Join(f.t.TempDir(), "pty"), os.O_CREATE|os.O_RDWR, 0644)
}

func (f *fakeTmux) Close() {}

func (f *fakeTmux) exec() cmd_test.MockCmdExec {
	return cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			switch {
			case strings.Contains(cmd.String(), "has-session") && !f.alive:
				return fmt.Errorf("can't find session")
			case strings.Contains(cmd.String(), "kill-session"):
				f.alive = false
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) { return nil, nil },
	}
}

// newInPlaceInstance starts an instance in a directory that is not a git repository.
func newInPlaceInstance(t *testing.T, title string) (*Instance, *fakeTmux) {
	dir := t.TempDir()
	require.False(t, git.IsGitRepo(dir), "test needs a directory outside any git repository")

	fake := &fakeTmux{t: t}
	instance, err := NewInstance(InstanceOptions{Title: title, Path: dir, Program: "claude"})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps(title, "claude", fake, fake.exec()))
	require.NoError(t, instance.Start(true))
	return instance, fake
}

// Outside a git repository there is nothing to make a worktree from, so the session runs
// directly in the directory claude-squad was started in.
func TestStartRunsInPlaceOutsideGitRepo(t *testing.T) {
	instance, fake := newInPlaceInstance(t, "inplace")

	require.Equal(t, Running, instance.Status)
	require.False(t, instance.HasWorktree())
	require.Empty(t, instance.Branch)
	require.Equal(t, instance.Path, instance.GetWorkDir())
	require.Len(t, fake.sessions, 1)
	require.Contains(t, fake.sessions[0], "-c "+instance.Path)

	repoName, err := instance.RepoName()
	require.NoError(t, err)
	require.Equal(t, filepath.Base(instance.Path), repoName)

	require.NoError(t, instance.UpdateDiffStats())
	require.Nil(t, instance.GetDiffStats())
	require.Nil(t, instance.ComputeDiff())
	require.Nil(t, instance.ComputeDiffNumstat())

	require.True(t, instance.InPlace())
	checkedOut, err := instance.IsBranchCheckedOut()
	require.NoError(t, err)
	require.False(t, checkedOut)
	require.Error(t, instance.Pause(), "checkout needs a branch to hand over")

	require.NoError(t, instance.Kill())
	require.False(t, fake.alive)
}

func TestStartOnBranchFailsOutsideGitRepo(t *testing.T) {
	instance, err := NewInstance(InstanceOptions{Title: "onbranch", Path: t.TempDir(), Program: "claude"})
	require.NoError(t, err)
	instance.SetSelectedBranch("main")

	require.ErrorContains(t, instance.Start(true), "not a git repository")
}

// An in-place instance whose tmux session died comes back Paused; Resume starts a new
// session in the same directory.
func TestResumeRestartsInPlaceInstanceInItsDirectory(t *testing.T) {
	instance, fake := newInPlaceInstance(t, "revive")

	fake.alive = false
	instance.SetStatus(Paused)

	require.NoError(t, instance.Resume())
	require.Equal(t, Running, instance.Status)
	require.Len(t, fake.sessions, 2)
	require.Contains(t, fake.sessions[1], "-c "+instance.Path)
}

func TestInPlaceInstanceRoundTripsThroughStorage(t *testing.T) {
	instance, _ := newInPlaceInstance(t, "stored")

	data := instance.ToInstanceData()
	require.Empty(t, data.Worktree.RepoPath)

	// Paused, so FromInstanceData does not try to reattach to a real tmux session.
	data.Status = Paused
	restored, err := FromInstanceData(data)
	require.NoError(t, err)
	require.False(t, restored.HasWorktree())
	require.Equal(t, instance.Path, restored.GetWorkDir())
	require.Nil(t, restored.GetDiffStats())
}

// newRepoDir creates a directory holding one committed git repository, the shape of a
// folder of checkouts like ~/Projects/FOYS/foys-all. HOME points at a temp directory so the
// workspace stays out of the real ~/.claude-squad.
func newRepoDir(t *testing.T) string {
	t.Setenv("HOME", t.TempDir())
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "test")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "test@example.com")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repoA")
	require.NoError(t, os.MkdirAll(repo, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0644))
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"commit", "-q", "-m", "init"}} {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	return root
}

// A Claude session started in a directory of repositories runs there, and gets a workspace
// that gives each repository a worktree once Claude changes it.
func TestStartUsesWorkspaceInDirectoryOfRepos(t *testing.T) {
	root := newRepoDir(t)
	fake := &fakeTmux{t: t}
	instance, err := NewInstance(InstanceOptions{Title: "multi", Path: root, Program: "claude"})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("multi", "claude", fake, fake.exec()))

	require.NoError(t, instance.Start(true))
	require.True(t, instance.IsMultiRepo())
	require.False(t, instance.InPlace())
	require.Equal(t, git.BranchNameFor("multi"), instance.Branch)
	require.Equal(t, instance.Path, instance.GetWorkDir(), "Claude runs in the real directory")
	require.Contains(t, fake.sessions[0], "-c "+instance.Path)
	require.Contains(t, instance.launchProgram(), "claude --settings ")

	// What the hook does on Claude's first edit in repoA.
	_, err = instance.workspace.Isolate("repoA")
	require.NoError(t, err)
	worktree := instance.workspace.WorktreePath("repoA")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "README.md"), []byte("goodbye\n"), 0644))

	stats := instance.ComputeDiff()
	require.NoError(t, stats.Error)
	require.Equal(t, []string{"repoA"}, stats.RepoNames())
	require.Contains(t, stats.Repos[0].Content, "a/README.md")

	data := instance.ToInstanceData()
	require.NotNil(t, data.Workspace)
	require.Empty(t, data.Worktree.RepoPath)
	data.Status = Paused
	restored, err := FromInstanceData(data)
	require.NoError(t, err)
	require.True(t, restored.IsMultiRepo())
	require.Nil(t, restored.ComputeDiffNumstat(), "paused instances report no diff")
	require.Contains(t, restored.launchProgram(), "--settings ")

	dir := instance.workspace.Dir()
	require.NoError(t, instance.Kill())
	require.NoDirExists(t, dir)
	require.NoDirExists(t, worktree)
}

// Isolation relies on Claude Code hooks; other agents run in place.
func TestNonClaudeProgramRunsInPlaceInDirectoryOfRepos(t *testing.T) {
	root := newRepoDir(t)
	fake := &fakeTmux{t: t}
	instance, err := NewInstance(InstanceOptions{Title: "codex", Path: root, Program: "codex"})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("codex", "codex", fake, fake.exec()))

	require.NoError(t, instance.Start(true))
	require.True(t, instance.InPlace())
	require.True(t, workspace.HasRepos(root))
}

// Checkout (pause) commits each isolated repository to its session branch and removes the
// worktrees; resume brings them back with the changes.
func TestPauseAndResumeMultiRepoInstance(t *testing.T) {
	root := newRepoDir(t)
	fake := &fakeTmux{t: t}
	instance, err := NewInstance(InstanceOptions{Title: "handoff", Path: root, Program: "claude"})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("handoff", "claude", fake, fake.exec()))
	require.NoError(t, instance.Start(true))

	_, err = instance.workspace.Isolate("repoA")
	require.NoError(t, err)
	worktree := instance.workspace.WorktreePath("repoA")
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "README.md"), []byte("goodbye\n"), 0644))

	require.NoError(t, instance.Pause())
	require.Equal(t, Paused, instance.Status)
	require.NoDirExists(t, worktree)
	out, err := exec.Command("git", "-C", filepath.Join(root, "repoA"), "show", instance.Branch+":README.md").CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Equal(t, "goodbye\n", string(out))

	require.NoError(t, instance.Resume())
	require.Equal(t, Running, instance.Status)
	require.FileExists(t, filepath.Join(worktree, "README.md"))
	require.Equal(t, []string{"repoA"}, instance.ComputeDiffNumstat().RepoNames())
	require.NoError(t, instance.Kill())
}
