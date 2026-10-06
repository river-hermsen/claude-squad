package session

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/log"
	"claude-squad/session/claudestatus"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"claude-squad/session/workspace"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Tests must not run the real `claude` to look for background sessions.
	stopBackgroundConversation = func(program, sessionID, name, cwd string) {}
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
	t         *testing.T
	alive     bool
	name      string   // the session's name, from new-session and rename-session
	sessions  []string // every new-session command, in order
	pty       string   // the file standing in for the latest PTY, which holds what was typed
	pane      string   // what capture-pane returns
	mouse     bool     // whether the pane's program takes mouse events
	altScreen bool     // whether the pane's program draws on the alternate screen
	panePID   string   // the pane's process id, or "" if tmux cannot tell
}

// argAfter returns the argument after flag in cmd, or "".
func argAfter(cmd *exec.Cmd, flag string) string {
	for i, arg := range cmd.Args[:len(cmd.Args)-1] {
		if arg == flag {
			return cmd.Args[i+1]
		}
	}
	return ""
}

func (f *fakeTmux) Start(cmd *exec.Cmd) (*os.File, error) {
	if strings.Contains(cmd.String(), "new-session") {
		f.alive = true
		f.name = argAfter(cmd, "-s")
		f.sessions = append(f.sessions, cmd.String())
	}
	f.pty = filepath.Join(f.t.TempDir(), "pty")
	return os.OpenFile(f.pty, os.O_CREATE|os.O_RDWR, 0644)
}

// typed returns what was written to the session's PTY.
func (f *fakeTmux) typed() string {
	data, err := os.ReadFile(f.pty)
	require.NoError(f.t, err)
	return string(data)
}

func (f *fakeTmux) Close() {}

func (f *fakeTmux) exec() cmd_test.MockCmdExec {
	return cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			switch {
			case strings.Contains(cmd.String(), "has-session"):
				target := strings.TrimPrefix(cmd.Args[len(cmd.Args)-1], "-t=")
				if !f.alive || (f.name != "" && target != f.name) {
					return fmt.Errorf("can't find session")
				}
			case strings.Contains(cmd.String(), "rename-session"):
				f.name = cmd.Args[len(cmd.Args)-1]
			case strings.Contains(cmd.String(), "kill-session"):
				f.alive = false
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			switch {
			case strings.Contains(cmd.String(), "capture-pane"):
				return []byte(f.pane), nil
			case strings.Contains(cmd.String(), "mouse_any_flag") && f.mouse:
				return []byte("1\n"), nil
			case strings.Contains(cmd.String(), "mouse_any_flag"):
				return []byte("0\n"), nil
			case strings.Contains(cmd.String(), "alternate_on") && f.altScreen:
				return []byte("1\n"), nil
			case strings.Contains(cmd.String(), "pane_pid") && f.panePID != "":
				return []byte(f.panePID + "\n"), nil
			case strings.Contains(cmd.String(), "pane_pid"):
				return nil, fmt.Errorf("no pane")
			}
			return nil, nil
		},
	}
}

// newInPlaceInstance starts an instance in a directory that is not a git repository.
func newInPlaceInstance(t *testing.T, title string) (*Instance, *fakeTmux) {
	// Claude Code settings go under HOME; keep them out of the real ~/.claude-squad.
	t.Setenv("HOME", t.TempDir())
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
	require.True(t, instance.InPlace())
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

// Claude Code reports its model, effort and context use to the status line claude-squad
// installs, in every kind of session.
func TestClaudeInstanceReportsStatus(t *testing.T) {
	instance, _ := newInPlaceInstance(t, "status")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	require.Contains(t, instance.launchProgram(), " --settings ")
	settings, err := os.ReadFile(filepath.Join(instance.claudeDir, "settings.json"))
	require.NoError(t, err)
	require.Contains(t, string(settings), "statusline --out")
	require.NotContains(t, string(settings), "hooks", "the isolation hook is only for multi-repo sessions")
	require.Nil(t, instance.ComputeClaudeInfo(), "nothing reported yet")

	input := `{"model":{"display_name":"Opus 4.7"},"effort":{"level":"high"},"context_window":{"total_input_tokens":1200,"context_window_size":200000}}`
	require.NoError(t, claudestatus.RunStatusLine(filepath.Join(instance.claudeDir, "status.json"), "", strings.NewReader(input), io.Discard))
	info := instance.ComputeClaudeInfo()
	require.NotNil(t, info)
	require.Equal(t, "Opus 4.7", info.Model)
	require.Equal(t, "high", info.Effort)
	require.Equal(t, 1200, info.ContextUsed)

	require.NoError(t, instance.Kill())
	require.NoDirExists(t, instance.claudeDir)
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
	require.Nil(t, data.Workspace)

	// Paused, so FromInstanceData does not try to reattach to a real tmux session.
	data.Status = Paused
	restored, err := FromInstanceData(data)
	require.NoError(t, err)
	require.True(t, restored.InPlace())
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
	require.Contains(t, instance.launchProgram(), " --settings ")
	settings, err := os.ReadFile(filepath.Join(instance.claudeDir, "settings.json"))
	require.NoError(t, err)
	require.Contains(t, string(settings), "hook --workspace", "the isolation hook comes with the session's settings")
	require.Contains(t, string(settings), "statusline --out")

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
	require.Empty(t, data.Workspace.SingleRepo)
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
	require.NoDirExists(t, instance.claudeDir)
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

// A session started inside a git repository gets a single-repo workspace: the repository's
// worktree is created up front and the agent runs inside it. Nothing goes to the old
// ~/.claude-squad/worktrees directory.
func TestStartUsesSingleRepoWorkspaceInsideGitRepo(t *testing.T) {
	root := newRepoDir(t)
	repo := filepath.Join(root, "repoA")
	fake := &fakeTmux{t: t}
	instance, err := NewInstance(InstanceOptions{Title: "single", Path: repo, Program: "codex"})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("single", "codex", fake, fake.exec()))

	require.NoError(t, instance.Start(true))
	require.False(t, instance.InPlace())
	require.False(t, instance.IsMultiRepo())
	require.Equal(t, git.BranchNameFor("single"), instance.Branch)
	worktree := instance.workspace.WorktreePath("repoA")
	require.Equal(t, worktree, instance.GetWorkDir())
	require.FileExists(t, filepath.Join(worktree, "README.md"))
	require.Contains(t, fake.sessions[0], "-c "+worktree, "the agent runs inside the worktree")
	require.Equal(t, "codex", instance.launchProgram(), "no hook: any agent works")
	repoName, err := instance.RepoName()
	require.NoError(t, err)
	require.Equal(t, "repoA", repoName)

	home, _ := os.UserHomeDir()
	require.NoDirExists(t, filepath.Join(home, ".claude-squad", "worktrees"))
	require.DirExists(t, filepath.Join(home, ".claude-squad", "workspaces"))

	require.NoError(t, os.WriteFile(filepath.Join(worktree, "README.md"), []byte("goodbye\n"), 0644))
	stats := instance.ComputeDiff()
	require.NoError(t, stats.Error)
	require.Contains(t, stats.Content, "+goodbye", "single-repo diffs are shown as a plain diff")

	data := instance.ToInstanceData()
	require.Equal(t, "repoA", data.Workspace.SingleRepo)
	data.Status = Paused
	restored, err := FromInstanceData(data)
	require.NoError(t, err)
	require.Equal(t, worktree, restored.GetWorkDir())

	require.NoError(t, instance.Kill())
	require.NoDirExists(t, worktree)
	out, _ := exec.Command("git", "-C", repo, "branch", "--list", instance.Branch).CombinedOutput()
	require.Empty(t, strings.TrimSpace(string(out)), "the session branch is deleted")
}

// Starting on an existing branch (the branch picker) keeps that branch when the session is killed.
func TestSingleRepoSessionOnExistingBranchKeepsIt(t *testing.T) {
	root := newRepoDir(t)
	repo := filepath.Join(root, "repoA")
	out, err := exec.Command("git", "-C", repo, "branch", "feature/x").CombinedOutput()
	require.NoError(t, err, "%s", out)

	fake := &fakeTmux{t: t}
	instance, err := NewInstance(InstanceOptions{Title: "picked", Path: repo, Program: "claude"})
	require.NoError(t, err)
	instance.SetSelectedBranch("feature/x")
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("picked", "claude", fake, fake.exec()))

	require.NoError(t, instance.Start(true))
	require.Equal(t, "feature/x", instance.Branch)
	out, err = exec.Command("git", "-C", instance.GetWorkDir(), "branch", "--show-current").CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Equal(t, "feature/x", strings.TrimSpace(string(out)))

	require.NoError(t, instance.Kill())
	out, _ = exec.Command("git", "-C", repo, "branch", "--list", "feature/x").CombinedOutput()
	require.Contains(t, string(out), "feature/x")
}

// Renaming changes the title and the tmux session's name, but not the directory of Claude
// Code settings, which a running Claude keeps using. The branch follows only while no
// repository has a worktree on it.
func TestRenameRenamesTmuxSession(t *testing.T) {
	root := newRepoDir(t)
	fake := &fakeTmux{t: t}
	instance, err := NewInstance(InstanceOptions{Title: "old-name", Path: root, Program: "claude"})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("old-name", "claude", fake, fake.exec()))
	require.NoError(t, instance.Start(true))
	claudeDir := instance.claudeDir

	require.NoError(t, instance.Rename("new-name"))
	require.Equal(t, "new-name", instance.Title)
	require.Equal(t, "claudesquad_new-name", fake.name)
	require.True(t, instance.TmuxAlive())
	require.Equal(t, git.BranchNameFor("new-name"), instance.Branch)

	_, err = instance.workspace.Isolate("repoA")
	require.NoError(t, err)
	require.NoError(t, instance.Rename("third"))
	require.Equal(t, git.BranchNameFor("new-name"), instance.Branch, "repoA's worktree keeps its branch")

	data := instance.ToInstanceData()
	require.Equal(t, "third", data.Title)
	require.Equal(t, claudeDir, data.ClaudeDir, "the settings directory is stored, not derived from the title")
	require.NoError(t, instance.Kill())
}

// An instance resuming a conversation opens Claude's picker without a name of its own, then
// takes the name of the conversation picked there.
func TestResumeConversationTakesPickedName(t *testing.T) {
	root := newRepoDir(t)
	fake := &fakeTmux{t: t}
	instance, err := NewInstance(InstanceOptions{Title: "resume", Path: root, Program: "claude"})
	require.NoError(t, err)
	instance.ResumeConversation()
	require.NoError(t, instance.setClaudeDir())
	require.Contains(t, instance.launchProgram(), " --resume")
	require.NotContains(t, instance.launchProgram(), "--name", "the picked conversation keeps its name")

	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("resume", "claude", fake, fake.exec()))
	require.NoError(t, instance.Start(true))
	require.True(t, instance.AdoptsClaudeName())
	require.Equal(t, git.BranchNameFor("resume"), instance.Branch)

	require.NoError(t, instance.AdoptClaudeName("fix the map", "fix the map"))
	require.False(t, instance.AdoptsClaudeName())
	require.Equal(t, "fix the map", instance.Title)
	require.Equal(t, "claudesquad_fixthemap", fake.name)
	require.Equal(t, git.BranchNameFor("fix the map"), instance.Branch, "no repository changed yet, so the branch follows")
	require.NotContains(t, instance.launchProgram(), "--resume", "a restart starts a new conversation")
	require.Contains(t, instance.launchProgram(), "--name 'fix the map'")

	fake.pane = "────\n❯ \n────\n"
	sent, err := instance.SyncClaudeName()
	require.NoError(t, err)
	require.False(t, sent, "Claude already has this name")
}

// A fork continues the source's conversation in a copy of its work: its worktree has the
// same uncommitted changes, on a branch of its own, and the source is left as it was.
func TestForkCopiesWorkAndConversation(t *testing.T) {
	root := newRepoDir(t)
	repo := filepath.Join(root, "repoA")
	srcFake := &fakeTmux{t: t}
	src, err := NewInstance(InstanceOptions{Title: "src", Path: repo, Program: "claude"})
	require.NoError(t, err)
	src.SetTmuxSession(tmux.NewTmuxSessionWithDeps("src", "claude", srcFake, srcFake.exec()))
	require.NoError(t, src.Start(true))

	_, err = NewFork(src, "src-fork")
	require.ErrorContains(t, err, "no conversation to fork yet")
	src.SetClaudeInfo(&claudestatus.Info{Model: "Opus 4.7", SessionID: "abc-123"})

	srcTree := src.GetWorkDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcTree, "README.md"), []byte("changed\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcTree, "new.txt"), []byte("new\n"), 0644))

	fork, err := NewFork(src, "src-fork")
	require.NoError(t, err)
	require.Contains(t, fork.launchArgs, "--resume 'abc-123' --fork-session")
	forkFake := &fakeTmux{t: t}
	fork.SetTmuxSession(tmux.NewTmuxSessionWithDeps("src-fork", "claude", forkFake, forkFake.exec()))
	require.NoError(t, fork.Start(true))

	forkTree := fork.GetWorkDir()
	require.NotEqual(t, srcTree, forkTree)
	require.Equal(t, git.BranchNameFor("src-fork"), fork.Branch)
	readme, err := os.ReadFile(filepath.Join(forkTree, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "changed\n", string(readme))
	require.FileExists(t, filepath.Join(forkTree, "new.txt"))

	stats := fork.ComputeDiff()
	require.NoError(t, stats.Error)
	require.Equal(t, 2, stats.Added, "the fork's diff shows the source's changes")

	out, err := exec.Command("git", "-C", srcTree, "status", "--porcelain").CombinedOutput()
	require.NoError(t, err)
	require.Equal(t, " M README.md\n?? new.txt\n", string(out), "the source is untouched")

	require.NoError(t, fork.Kill())
	require.NoDirExists(t, forkTree)
	require.FileExists(t, filepath.Join(srcTree, "new.txt"))
	require.NoError(t, src.Kill())
}

// The Claude Code session carries the instance's name, and takes a new one on rename, but
// only once it is idle with an empty prompt so nothing the user typed gets mixed in.
func TestClaudeSessionFollowsInstanceName(t *testing.T) {
	instance, fake := newInPlaceInstance(t, "first name")
	require.Contains(t, instance.launchProgram(), "claude --name 'first name' --settings ")

	require.NoError(t, instance.Rename("second"))
	require.Contains(t, instance.launchProgram(), "--name 'second'", "a restart uses the new name")

	fake.pane = "────\n\x1b[39m❯ draft I am typing\x1b[7m \x1b[27m\n────\n"
	sent, err := instance.SyncClaudeName()
	require.NoError(t, err)
	require.False(t, sent, "the user is typing")
	require.Empty(t, fake.typed())

	fake.pane = "──── first name ─\n\x1b[39m❯ \x1b[2mTry \"create a util\"\x1b[0m\n────\n  status line\n"
	sent, err = instance.SyncClaudeName()
	require.NoError(t, err)
	require.True(t, sent)
	require.Equal(t, "/rename second\r", fake.typed())

	sent, err = instance.SyncClaudeName()
	require.NoError(t, err)
	require.False(t, sent, "sent once")
}

// Scrolling goes to a program that takes mouse events, as a wheel event in the middle of the
// pane, and otherwise is left to the preview.
func TestScrollSessionSendsWheelToMouseProgram(t *testing.T) {
	instance, fake := newInPlaceInstance(t, "scroll")
	require.False(t, instance.ScrollSession(true), "classic UI: scroll tmux's scrollback instead")
	require.Empty(t, fake.typed())

	other, fake := newInPlaceInstance(t, "scroll-full")
	fake.mouse = true
	require.True(t, other.ScrollSession(true))
	require.True(t, other.ScrollSession(false))
	// The fake PTY has no size, so the middle of a default 80x24 terminal.
	require.Equal(t, "\x1b[<64;41;13M\x1b[<65;41;13M", fake.typed())
}

// Background sessions dispatched from an instance's Claude report to its status line too. The
// list shows the conversation running in the instance's tmux pane, which Claude Code's
// session registry names.
func TestClaudeInfoComesFromPaneConversation(t *testing.T) {
	instance, fake := newInPlaceInstance(t, "pane")
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	report := func(id, name string, context int) {
		input := fmt.Sprintf(`{"model":{"display_name":"Opus 4.7"},"session_id":%q,"session_name":%q,"context_window":{"total_input_tokens":%d,"context_window_size":1000000}}`, id, name, context)
		require.NoError(t, claudestatus.RunStatusLine(filepath.Join(instance.claudeDir, "status.json"), "", strings.NewReader(input), io.Discard))
	}
	report("main-conversation", "pane", 147000)
	report("agent-conversation", "agent", 0)
	require.Equal(t, "agent", instance.ComputeClaudeInfo().SessionName, "without the registry, the latest report")

	fake.panePID = "50251"
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, "sessions"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "sessions", "50251.json"),
		[]byte(`{"pid":50251,"sessionId":"main-conversation","kind":"interactive"}`), 0644))
	info := instance.ComputeClaudeInfo()
	require.Equal(t, "pane", info.SessionName)
	require.Equal(t, 147000, info.ContextUsed)
}

// Claude Code's fullscreen UI without mouse events, as cs runs it by default, scrolls a page at
// a time with Page Up and Page Down; its output is not in tmux's scrollback.
func TestScrollSessionPagesFullscreenClaude(t *testing.T) {
	instance, fake := newInPlaceInstance(t, "pages")
	fake.altScreen = true
	require.True(t, instance.ScrollSession(true))
	require.True(t, instance.ScrollSession(false))
	require.Equal(t, "\x1b[5~\x1b[6~", fake.typed())

	other, err := NewInstance(InstanceOptions{Title: "codex", Path: t.TempDir(), Program: "codex"})
	require.NoError(t, err)
	otherFake := &fakeTmux{t: t, altScreen: true}
	other.SetTmuxSession(tmux.NewTmuxSessionWithDeps("codex", "codex", otherFake, otherFake.exec()))
	require.NoError(t, other.Start(true))
	require.False(t, other.ScrollSession(true), "only Claude Code is known to page with these keys")
}

// Claude Code gets the mouse only if cs is set to take it; otherwise its fullscreen UI would
// copy a selection to a tmux buffer instead of the terminal's clipboard.
func TestClaudeMouseFollowsSetting(t *testing.T) {
	defer func(saved bool) { tmux.CaptureMouse = saved }(tmux.CaptureMouse)
	instance, _ := newInPlaceInstance(t, "mouse")

	tmux.CaptureMouse = false
	require.True(t, strings.HasPrefix(instance.launchProgram(), "CLAUDE_CODE_DISABLE_MOUSE=1 claude --name 'mouse' "), instance.launchProgram())
	tmux.CaptureMouse = true
	require.True(t, strings.HasPrefix(instance.launchProgram(), "claude --name 'mouse' "), instance.launchProgram())

	codex, err := NewInstance(InstanceOptions{Title: "codex", Path: t.TempDir(), Program: "codex"})
	require.NoError(t, err)
	tmux.CaptureMouse = false
	require.Equal(t, "codex", codex.launchProgram())
}
