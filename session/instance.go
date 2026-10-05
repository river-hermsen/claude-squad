package session

import (
	"claude-squad/log"
	"claude-squad/session/claudestatus"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"claude-squad/session/workspace"
	"errors"
	"path/filepath"

	"fmt"
	"os"
	"strings"
	"time"

	"github.com/atotto/clipboard"
)

type Status int

const (
	// Running is the status when the instance is running and claude is working.
	Running Status = iota
	// Ready is if the claude instance is ready to be interacted with (waiting for user input).
	Ready
	// Loading is if the instance is loading (if we are starting it up or something).
	Loading
	// Paused is if the instance is paused (worktree removed but branch preserved).
	Paused
)

// Instance is a running instance of claude code.
type Instance struct {
	// Title is the title of the instance.
	Title string
	// Path is the path to the workspace.
	Path string
	// Branch is the branch of the instance.
	Branch string
	// Status is the status of the instance.
	Status Status
	// Program is the program to run in the instance.
	Program string
	// Height is the height of the instance.
	Height int
	// Width is the width of the instance.
	Width int
	// CreatedAt is the time the instance was created.
	CreatedAt time.Time
	// UpdatedAt is the time the instance was last updated.
	UpdatedAt time.Time
	// AutoYes is true if the instance should automatically press enter when prompted.
	AutoYes bool
	// Prompt is the initial prompt to pass to the instance on startup
	Prompt string
	// RemoteControl is true if the Claude Code session runs with Remote Control on, so that
	// claude.ai/code and the Claude app can continue it; see remote_control.go.
	RemoteControl bool

	// DiffStats stores the current git diff statistics
	diffStats *git.DiffStats

	// selectedBranch is the existing branch to start on (empty = new branch from HEAD)
	selectedBranch string

	// The below fields are initialized upon calling Start().

	started bool
	// tmuxSession is the tmux session for the instance.
	tmuxSession *tmux.TmuxSession
	// workspace holds the instance's git worktrees: one for an instance started inside a
	// git repository, one per changed repository for an instance started in a directory of
	// repositories. Without one, the instance runs in place in Path.
	workspace *workspace.Workspace
	// claudeDir holds the settings claude-squad passes to Claude Code and the status Claude
	// reports back (see claudestatus), or is "" when the program is not Claude Code.
	claudeDir string
	// claudeInfo is the model, effort and context use Claude Code last reported.
	claudeInfo *claudestatus.Info
	// launchArgs are extra Claude Code arguments for the first start only: --resume for a
	// conversation picked when the instance was created, or the conversation a fork continues.
	launchArgs string
	// forkOf is the instance this one is a fork of, until Start has copied its work.
	forkOf *Instance
	// pendingClaudeName is a name that Rename gave the instance and the running Claude Code
	// session has not taken yet; see SyncClaudeName.
	pendingClaudeName string
	// adoptsClaudeName is true for an instance resuming a conversation picked in Claude Code,
	// until it has taken that conversation's name as its title; see AdoptClaudeName.
	adoptsClaudeName bool
	// claudeSessionID is the conversation last seen running in the tmux pane. Restarting the
	// instance after its tmux session ended continues it; see Resume.
	claudeSessionID string
	// resumedAt is when Resume last restarted the instance on claudeSessionID, or zero if it
	// started it afresh; see MarkEnded.
	resumedAt time.Time

	// remoteSessionID is the claude.ai session Remote Control connects the running Claude Code
	// session to, or "" while Remote Control is off; see SetClaudeProcess.
	remoteSessionID string
	// remoteSince is when Remote Control was last asked for, by starting the session with it or
	// switching it on. RemoteState counts it as failed if it is still off well after.
	remoteSince time.Time
	// pendingRemoteControl is true while `/remote-control` waits for Claude Code to be idle;
	// see SyncRemoteControl.
	pendingRemoteControl bool
}

// ToInstanceData converts an Instance to its serializable form
func (i *Instance) ToInstanceData() InstanceData {
	data := InstanceData{
		Title:     i.Title,
		Path:      i.Path,
		Branch:    i.Branch,
		Status:    i.Status,
		Height:    i.Height,
		Width:     i.Width,
		CreatedAt: i.CreatedAt,
		UpdatedAt: time.Now(),
		Program:   i.Program,
		AutoYes:   i.AutoYes,
		ClaudeDir: i.claudeDir,

		RemoteControl:   i.RemoteControl,
		ClaudeSessionID: i.claudeSessionID,
	}

	if i.workspace != nil {
		data.Workspace = &WorkspaceData{
			Root:       i.workspace.Root(),
			Dir:        i.workspace.Dir(),
			BranchName: i.workspace.BranchName(),
			SingleRepo: i.workspace.SingleRepo(),
		}
	}

	// Only include diff stats if they exist
	if i.diffStats != nil {
		data.DiffStats = DiffStatsData{
			Added:   i.diffStats.Added,
			Removed: i.diffStats.Removed,
			Content: i.diffStats.Content,
		}
	}

	return data
}

// FromInstanceData creates a new Instance from serialized data
func FromInstanceData(data InstanceData) (*Instance, error) {
	instance := &Instance{
		Title:     data.Title,
		Path:      data.Path,
		Branch:    data.Branch,
		Status:    data.Status,
		Height:    data.Height,
		Width:     data.Width,
		CreatedAt: data.CreatedAt,
		UpdatedAt: data.UpdatedAt,
		Program:   data.Program,

		RemoteControl:   data.RemoteControl,
		claudeSessionID: data.ClaudeSessionID,
	}

	if data.Workspace != nil {
		instance.workspace = workspace.FromStorage(
			data.Workspace.Root, data.Workspace.Dir, data.Title, data.Workspace.BranchName, data.Workspace.SingleRepo)
	}
	instance.claudeDir = data.ClaudeDir
	if instance.claudeDir == "" && instance.IsClaude() {
		// Saved before the directory name was stored: it was the title.
		dir, err := claudestatus.Dir(instance.Title)
		if err != nil {
			return nil, err
		}
		instance.claudeDir = dir
	}
	instance.claudeInfo = instance.ComputeClaudeInfo()

	if instance.Paused() {
		instance.started = true
		instance.tmuxSession = tmux.NewTmuxSession(instance.Title, instance.launchProgram())
	} else {
		if err := instance.Start(false); err != nil {
			return nil, err
		}
	}

	return instance, nil
}

// Options for creating a new instance
type InstanceOptions struct {
	// Title is the title of the instance.
	Title string
	// Path is the path to the workspace.
	Path string
	// Program is the program to run in the instance (e.g. "claude", "aider --model ollama_chat/gemma3:1b")
	Program string
	// If AutoYes is true, then
	AutoYes bool
	// Branch is an existing branch name to start the session on (empty = new branch from HEAD)
	Branch string
	// RemoteControl starts a Claude Code session with Remote Control on.
	RemoteControl bool
}

func NewInstance(opts InstanceOptions) (*Instance, error) {
	t := time.Now()

	// Convert path to absolute
	absPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	return &Instance{
		Title:          opts.Title,
		Status:         Ready,
		Path:           absPath,
		Program:        opts.Program,
		Height:         0,
		Width:          0,
		CreatedAt:      t,
		UpdatedAt:      t,
		AutoYes:        false,
		RemoteControl:  opts.RemoteControl,
		selectedBranch: opts.Branch,
	}, nil
}

func (i *Instance) RepoName() (string, error) {
	if !i.started {
		return "", fmt.Errorf("cannot get repo name for instance that has not been started")
	}
	if i.workspace != nil && i.workspace.SingleRepo() != "" {
		return i.workspace.SingleRepo(), nil
	}
	return filepath.Base(i.Path), nil
}

// IsMultiRepo reports whether the instance runs in a directory of git repositories, each
// of which gets a worktree once the agent changes it. Always false before Start.
func (i *Instance) IsMultiRepo() bool {
	return i.workspace != nil && i.workspace.SingleRepo() == ""
}

// InPlace reports whether the instance runs directly in Path with no git isolation at all:
// no branch, diff or checkout. True before Start.
func (i *Instance) InPlace() bool {
	return i.workspace == nil
}

// IsClaude reports whether the instance runs Claude Code.
func (i *Instance) IsClaude() bool {
	return IsClaudeProgram(i.Program)
}

// IsClaudeProgram reports whether program, a command line, runs Claude Code.
func IsClaudeProgram(program string) bool {
	return workspace.SupportsProgram(program)
}

// launchProgram returns the command the tmux session runs: the program, plus for Claude Code
// the session name, Remote Control, the settings writeClaudeSettings writes, the workspace's
// arguments and launchArgs.
func (i *Instance) launchProgram() string {
	return i.programWith(i.launchArgs)
}

// programWith is launchProgram with args in place of launchArgs.
func (i *Instance) programWith(args string) string {
	program := i.Program
	if i.claudeDir == "" {
		return program
	}
	if !i.adoptsClaudeName {
		program += " --name " + shellQuote(i.Title)
	}
	if i.RemoteControl {
		program += " --remote-control"
	}
	program += " " + claudestatus.Args(i.claudeDir)
	if i.workspace != nil {
		if args := i.workspace.ClaudeArgs(); args != "" {
			program += " " + args
		}
	}
	if args != "" {
		program += " " + args
	}
	return program
}

// restartArgs returns the Claude Code arguments for restarting the instance after its tmux
// session ended: --resume of the conversation it ran, if Claude Code saved it. Continuing the
// conversation also reconnects its Remote Control session on claude.ai, so the same link
// keeps working. Otherwise it is launchArgs, and resumed is false.
func (i *Instance) restartArgs() (args string, resumed bool) {
	if i.claudeDir != "" && claudestatus.HasTranscript(i.claudeSessionID) {
		return "--resume " + shellQuote(i.claudeSessionID), true
	}
	return i.launchArgs, false
}

// setClaudeDir gives a new Claude instance its own directory for Claude Code settings.
func (i *Instance) setClaudeDir() error {
	i.claudeDir = ""
	if !i.IsClaude() {
		return nil
	}
	dir, err := claudestatus.NewDir(i.Title)
	if err != nil {
		return fmt.Errorf("failed to find the directory for Claude Code settings: %w", err)
	}
	i.claudeDir = dir
	return nil
}

// ResumeConversation makes Start continue an earlier Claude Code conversation instead of
// starting a new one: Claude opens its own picker, the way `claude --resume` does. The
// instance then takes the picked conversation's name; see AdoptClaudeName.
func (i *Instance) ResumeConversation() {
	i.launchArgs = "--resume"
	i.adoptsClaudeName = true
}

// AdoptsClaudeName reports whether the instance is waiting to take the name of the
// conversation picked in Claude Code; see ResumeConversation.
func (i *Instance) AdoptsClaudeName() bool {
	return i.adoptsClaudeName
}

// AdoptClaudeName renames the instance to title, made from the name of the conversation
// picked in Claude Code, which was claudeName. Claude is only told about the title if it
// had to differ from that name.
func (i *Instance) AdoptClaudeName(title string, claudeName string) error {
	i.adoptsClaudeName = false
	i.launchArgs = ""
	if err := i.Rename(title); err != nil {
		return err
	}
	if title == claudeName {
		i.pendingClaudeName = ""
	}
	return nil
}

// NewFork returns a new instance titled title that continues src's Claude Code conversation
// in a copy of its work: Start copies src's worktrees, with their uncommitted changes, onto
// a new branch, and runs `claude --resume <conversation> --fork-session` there.
func NewFork(src *Instance, title string) (*Instance, error) {
	if !src.started || src.claudeDir == "" {
		return nil, fmt.Errorf("only a started Claude Code session can be forked")
	}
	if src.claudeInfo == nil || src.claudeInfo.SessionID == "" {
		return nil, fmt.Errorf("session '%s' has no conversation to fork yet: send it a message first", src.Title)
	}
	fork, err := NewInstance(InstanceOptions{Title: title, Path: src.Path, Program: src.Program})
	if err != nil {
		return nil, err
	}
	fork.forkOf = src
	fork.launchArgs = "--resume " + shellQuote(src.claudeInfo.SessionID) + " --fork-session"
	return fork, nil
}

// Rename changes the title of a started instance and renames its tmux session to match.
// A running Claude Code session takes the new name as soon as it is idle; see
// SyncClaudeName. The branch follows too, until the session has changed a repository.
func (i *Instance) Rename(title string) error {
	if title == "" {
		return fmt.Errorf("title cannot be empty")
	}
	if i.tmuxSession != nil {
		if err := i.tmuxSession.Rename(title); err != nil {
			return err
		}
	}
	i.Title = title
	if i.workspace != nil {
		// Until the session has changed a repository, its branch can follow the name.
		if renamed, err := i.workspace.Rename(title); err != nil {
			log.WarningLog.Printf("could not rename the branch of %s: %v", title, err)
		} else if renamed {
			i.Branch = i.workspace.BranchName()
		}
	}
	if i.tmuxSession != nil {
		// A restarted session starts with the new name.
		i.tmuxSession.SetProgram(i.launchProgram())
	}
	if i.IsClaude() {
		i.pendingClaudeName = title
	}
	return nil
}

// SessionEnded reports whether the instance's tmux session is gone, as when the program in it
// exits. Safe to call from a background goroutine after HasUpdated.
func (i *Instance) SessionEnded() bool {
	return i.started && i.Status != Paused && i.tmuxSession != nil && i.tmuxSession.Ended()
}

// SyncClaudeName gives the running Claude Code session the name Rename set, by sending it
// `/rename <name>`. That only happens while Claude is waiting with an empty prompt, so it
// never mixes with what the user is typing; until then it reports false and waits for the
// next call. Call it from the main event loop when the instance is idle.
func (i *Instance) SyncClaudeName() (bool, error) {
	if i.pendingClaudeName == "" || !i.started || i.Status == Paused || i.tmuxSession == nil {
		return false, nil
	}
	content, err := i.tmuxSession.CapturePaneContent()
	if err != nil || !claudePromptIsEmpty(content) {
		return false, err
	}
	name := i.pendingClaudeName
	i.pendingClaudeName = ""
	return true, i.SendPrompt("/rename " + name)
}

// ScrollSession scrolls the program in the session itself, one mouse wheel step up or down,
// if it takes mouse events, as Claude Code's fullscreen UI does. It reports whether it did;
// otherwise the session's output is in tmux's scrollback, which the preview scrolls.
func (i *Instance) ScrollSession(up bool) bool {
	if !i.started || i.Status == Paused || i.tmuxSession == nil || !i.tmuxSession.WantsMouse() {
		return false
	}
	if err := i.tmuxSession.ScrollWheel(up); err != nil {
		log.WarningLog.Printf("could not scroll session %s: %v", i.Title, err)
		return false
	}
	return true
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// writeClaudeSettings writes the Claude Code settings launchProgram passes: claude-squad's
// status line, and in multi-repo instances the workspace's isolation hook. It must run
// before every start of the tmux session.
func (i *Instance) writeClaudeSettings() error {
	if i.claudeDir == "" {
		return nil
	}
	csPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find the claude-squad binary for Claude Code settings: %w", err)
	}
	var hooks map[string]any
	if i.IsMultiRepo() {
		hooks = i.workspace.Hooks(csPath)
	}
	if err := claudestatus.WriteSettings(i.claudeDir, csPath, hooks); err != nil {
		return fmt.Errorf("failed to write Claude Code settings: %w", err)
	}
	return nil
}

func (i *Instance) SetStatus(status Status) {
	i.Status = status
}

// SetSelectedBranch sets the branch to use when starting the instance.
func (i *Instance) SetSelectedBranch(branch string) {
	i.selectedBranch = branch
}

// firstTimeSetup is true if this is a new instance. Otherwise, it's one loaded from storage.
func (i *Instance) Start(firstTimeSetup bool) error {
	if i.Title == "" {
		return fmt.Errorf("instance title cannot be empty")
	}

	if firstTimeSetup {
		var ws *workspace.Workspace
		var err error
		switch {
		case i.forkOf != nil && i.forkOf.workspace != nil:
			ws, err = workspace.Fork(i.forkOf.workspace, i.Title)
		case git.IsGitRepo(i.Path):
			ws, err = workspace.NewSingle(i.Path, i.Title, i.selectedBranch)
		case i.selectedBranch != "":
			return fmt.Errorf("cannot start on branch %q: %s is not a git repository", i.selectedBranch, i.Path)
		// A directory of repositories gets a worktree per repository the agent changes,
		// which needs Claude Code's hooks. Anything else runs in place.
		case workspace.HasRepos(i.Path) && workspace.SupportsProgram(i.Program):
			ws, err = workspace.New(i.Path, i.Title)
		}
		if err != nil {
			return fmt.Errorf("failed to create workspace: %w", err)
		}
		if ws != nil {
			i.workspace = ws
			i.Branch = ws.BranchName()
		}
		if err := i.setClaudeDir(); err != nil {
			return err
		}
	}

	var tmuxSession *tmux.TmuxSession
	if i.tmuxSession != nil {
		// Use existing tmux session (useful for testing)
		tmuxSession = i.tmuxSession
	} else {
		// Create new tmux session
		tmuxSession = tmux.NewTmuxSession(i.Title, i.launchProgram())
	}
	i.tmuxSession = tmuxSession

	// Setup error handler to cleanup resources on any error
	var setupErr error
	defer func() {
		if setupErr != nil {
			if cleanupErr := i.Kill(); cleanupErr != nil {
				setupErr = fmt.Errorf("%v (cleanup error: %v)", setupErr, cleanupErr)
			}
		} else {
			i.started = true
		}
	}()

	if !firstTimeSetup {
		// Reuse existing session. If the tmux server died since we last ran (reboot,
		// crash, `tmux kill-server`), the session is gone but the worktree and branch
		// are still on disk. Park the instance as Paused so Resume can rebuild it.
		// Reporting an error here would be worse than useless: LoadInstances aborts on
		// the first failure, so a single dead session would hide every other instance.
		if err := tmuxSession.Restore(); err != nil {
			if errors.Is(err, tmux.ErrSessionNotFound) {
				log.WarningLog.Printf(
					"tmux session for %q no longer exists; pausing instance so it can be resumed", i.Title)
				i.SetStatus(Paused)
				return nil
			}
			setupErr = fmt.Errorf("failed to restore existing session: %w", err)
			return setupErr
		}
	} else {
		// Set up the workspace (and with it any worktree the agent runs in) first
		if i.workspace != nil {
			if err := i.workspace.Setup(); err != nil {
				setupErr = fmt.Errorf("failed to setup workspace: %w", err)
				if cleanupErr := i.workspace.Cleanup(); cleanupErr != nil {
					setupErr = fmt.Errorf("%v (cleanup error: %v)", setupErr, cleanupErr)
				}
				return setupErr
			}
		}
		if err := i.writeClaudeSettings(); err != nil {
			setupErr = err
			return setupErr
		}

		// Create new session
		if err := i.tmuxSession.Start(i.GetWorkDir()); err != nil {
			// Cleanup the workspace if tmux session creation fails
			if i.workspace != nil {
				if cleanupErr := i.workspace.Cleanup(); cleanupErr != nil {
					err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
				}
			}
			setupErr = fmt.Errorf("failed to start new session: %w", err)
			return setupErr
		}
		// launchArgs only apply to this first start; a later restart starts afresh, except
		// that an instance still waiting for a conversation to be picked opens the picker again.
		if !i.adoptsClaudeName {
			i.launchArgs = ""
		}
		i.forkOf = nil
		i.tmuxSession.SetProgram(i.launchProgram())
	}

	i.remoteSince = time.Now()
	i.SetStatus(Running)

	return nil
}

// Kill terminates the instance and cleans up all resources
func (i *Instance) Kill() error {
	if !i.started {
		// If instance was never started, just return success
		return nil
	}

	var errs []error

	// Claude Code may move the conversation to the background when its tmux session goes, so
	// find out which conversation it is first.
	sessionID := i.paneSessionID()
	if sessionID == "" && i.claudeInfo != nil {
		sessionID = i.claudeInfo.SessionID
	}

	// Always try to cleanup both resources, even if one fails
	// Clean up tmux session first since it's using the git worktree
	if i.tmuxSession != nil {
		if err := i.tmuxSession.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close tmux session: %w", err))
		}
	}

	// Then clean up the worktrees
	if i.workspace != nil {
		if err := i.workspace.Cleanup(); err != nil {
			errs = append(errs, fmt.Errorf("failed to cleanup workspace: %w", err))
		}
	}

	if i.claudeDir != "" {
		stopBackgroundConversation(i.Program, sessionID, i.Title, i.Path)
		if err := claudestatus.Remove(i.claudeDir); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove Claude Code settings: %w", err))
		}
	}

	return i.combineErrors(errs)
}

// combineErrors combines multiple errors into a single error
func (i *Instance) combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	errMsg := "multiple cleanup errors occurred:"
	for _, err := range errs {
		errMsg += "\n  - " + err.Error()
	}
	return fmt.Errorf("%s", errMsg)
}

func (i *Instance) Preview() (string, error) {
	if !i.started || i.Status == Paused {
		return "", nil
	}
	content, err := i.tmuxSession.CapturePaneContent()
	if err != nil && !i.tmuxSession.DoesSessionExist() {
		// The program exited, taking its tmux session with it. That is no error: the next
		// metadata tick pauses the instance.
		return "", nil
	}
	return content, err
}

func (i *Instance) HasUpdated() (updated bool, hasPrompt bool) {
	if !i.started {
		return false, false
	}
	return i.tmuxSession.HasUpdated()
}

// CheckAndHandleTrustPrompt checks for and dismisses the trust prompt for supported programs.
func (i *Instance) CheckAndHandleTrustPrompt() bool {
	if !i.started || i.tmuxSession == nil {
		return false
	}
	program := i.Program
	if !strings.HasSuffix(program, tmux.ProgramClaude) &&
		!strings.HasSuffix(program, tmux.ProgramAider) &&
		!strings.HasSuffix(program, tmux.ProgramGemini) {
		return false
	}
	return i.tmuxSession.CheckAndHandleTrustPrompt()
}

// TapEnter sends an enter key press to the tmux session if AutoYes is enabled.
func (i *Instance) TapEnter() {
	if !i.started || !i.AutoYes {
		return
	}
	if err := i.tmuxSession.TapEnter(); err != nil {
		log.ErrorLog.Printf("error tapping enter: %v", err)
	}
}

func (i *Instance) Attach() (chan struct{}, error) {
	if !i.started {
		return nil, fmt.Errorf("cannot attach instance that has not been started")
	}
	return i.tmuxSession.Attach()
}

func (i *Instance) SetPreviewSize(width, height int) error {
	if !i.started || i.Status == Paused {
		return fmt.Errorf("cannot set preview size for instance that has not been started or " +
			"is paused")
	}
	return i.tmuxSession.SetDetachedSize(width, height)
}

// IsBranchCheckedOut reports whether the instance's branch is checked out in the original
// repository (in any of them, for a multi-repo instance). The branch can then be neither
// deleted nor given a worktree again.
func (i *Instance) IsBranchCheckedOut() (bool, error) {
	if !i.started {
		return false, fmt.Errorf("cannot check branch of instance that has not been started")
	}
	if i.workspace == nil {
		return false, nil
	}
	repos, err := i.workspace.CheckedOutRepos()
	return len(repos) > 0, err
}

// GetWorkDir returns the directory the agent runs in: the worktree of a single-repo
// instance, otherwise Path.
func (i *Instance) GetWorkDir() string {
	if i.workspace != nil && i.workspace.SingleRepo() != "" {
		return i.workspace.WorktreePath(i.workspace.SingleRepo())
	}
	return i.Path
}

func (i *Instance) Started() bool {
	return i.started
}

// SetTitle sets the title of the instance. Returns an error if the instance has started.
// We cant change the title once it's been used for a tmux session etc.
func (i *Instance) SetTitle(title string) error {
	if i.started {
		return fmt.Errorf("cannot change title of a started instance")
	}
	i.Title = title
	return nil
}

func (i *Instance) Paused() bool {
	return i.Status == Paused
}

// TmuxAlive returns true if the tmux session is alive. This is a sanity check before attaching.
func (i *Instance) TmuxAlive() bool {
	return i.tmuxSession.DoesSessionExist()
}

// Pause commits the changes in every worktree to its branch, removes the worktrees while
// keeping the branches, and detaches the tmux session.
func (i *Instance) Pause() error {
	if !i.started {
		return fmt.Errorf("cannot pause instance that has not been started")
	}
	if i.Status == Paused {
		return fmt.Errorf("instance is already paused")
	}
	if i.InPlace() {
		return fmt.Errorf("cannot check out session '%s': it is not in a git repository", i.Title)
	}

	commitMsg := fmt.Sprintf("[claudesquad] update from '%s' on %s (paused)", i.Title, time.Now().Format(time.RFC822))
	if err := i.workspace.Pause(commitMsg); err != nil {
		// Some worktrees may still hold uncommitted work; keep the session running.
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to pause workspace: %w", err)
	}
	// Detach from tmux session instead of closing to preserve session output
	if err := i.tmuxSession.DetachSafely(); err != nil {
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to detach tmux session: %w", err)
	}
	i.SetStatus(Paused)
	_ = clipboard.WriteAll(i.workspace.BranchName())
	return nil
}

// Resume recreates the worktree and restarts the tmux session
func (i *Instance) Resume() error {
	if !i.started {
		return fmt.Errorf("cannot resume instance that has not been started")
	}
	if i.Status != Paused {
		return fmt.Errorf("can only resume paused instances")
	}

	if i.workspace != nil {
		if repos, err := i.workspace.CheckedOutRepos(); err != nil {
			return fmt.Errorf("failed to check if branches are checked out: %w", err)
		} else if len(repos) > 0 {
			return fmt.Errorf("cannot resume: branch %s is checked out in %s, please switch to a different branch",
				i.workspace.BranchName(), strings.Join(repos, ", "))
		}
		if err := i.workspace.Resume(); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to restore workspace: %w", err)
		}
	}

	// Without a workspace there is nothing on disk to rebuild, only the tmux session: such an
	// instance can only be paused by its tmux session dying, never by checkout.

	if err := i.writeClaudeSettings(); err != nil {
		log.ErrorLog.Print(err)
		return err
	}

	// Check if tmux session still exists from pause, otherwise create new one
	if i.tmuxSession.DoesSessionExist() {
		// Session exists, just restore PTY connection to it
		if err := i.tmuxSession.Restore(); err != nil {
			log.ErrorLog.Print(err)
			// If restore fails, fall back to creating new session
			if err := i.restart(); err != nil {
				log.ErrorLog.Print(err)
				return fmt.Errorf("failed to start new session: %w", err)
			}
		}
	} else {
		// Create new tmux session
		if err := i.restart(); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to start new session: %w", err)
		}
	}

	i.remoteSince = time.Now()
	i.SetStatus(Running)
	return nil
}

// restart starts a new tmux session for the instance, continuing its conversation if it can;
// see restartArgs.
func (i *Instance) restart() error {
	args, resumed := i.restartArgs()
	i.tmuxSession.SetProgram(i.programWith(args))
	if err := i.tmuxSession.Start(i.GetWorkDir()); err != nil {
		return err
	}
	i.resumedAt = time.Time{}
	if resumed {
		i.resumedAt = time.Now()
	}
	return nil
}

// UpdateDiffStats updates the git diff statistics for this instance
func (i *Instance) UpdateDiffStats() error {
	if !i.started || i.InPlace() {
		i.diffStats = nil
		return nil
	}

	if i.Status == Paused {
		// Keep the previous diff stats if the instance is paused
		return nil
	}

	stats := i.workspace.Diff()
	if stats.Error != nil {
		return fmt.Errorf("failed to get diff stats: %w", stats.Error)
	}
	i.diffStats = stats
	return nil
}

// ComputeDiff runs the expensive git diff I/O and returns the result without
// mutating instance state. Safe to call from a background goroutine.
func (i *Instance) ComputeDiff() *git.DiffStats {
	if !i.started || i.Status == Paused || i.InPlace() {
		return nil
	}
	return i.workspace.Diff()
}

// ComputeDiffNumstat runs a lightweight git diff --numstat and returns only the
// added/removed line counts (Content is left empty). Safe to call from a
// background goroutine. Use this for instances whose full diff content is not
// currently needed so we avoid keeping large diffs in memory.
func (i *Instance) ComputeDiffNumstat() *git.DiffStats {
	if !i.started || i.Status == Paused || i.InPlace() {
		return nil
	}
	return i.workspace.DiffNumstat()
}

// ComputeClaudeInfo reads what Claude Code last reported about the instance, or nil if it has
// reported nothing or the program is not Claude Code. Safe to call from a background goroutine.
func (i *Instance) ComputeClaudeInfo() *claudestatus.Info {
	if i.claudeDir == "" {
		return nil
	}
	// Background sessions dispatched from this one report to the same status line. Only the
	// conversation in the tmux pane counts, if Claude Code says which one that is.
	if id := i.paneSessionID(); id != "" {
		return claudestatus.ReadSession(i.claudeDir, id)
	}
	return claudestatus.Read(i.claudeDir)
}

// paneSessionID returns the id of the Claude Code conversation running in the instance's tmux
// pane, or "" if it cannot tell.
func (i *Instance) paneSessionID() string {
	if p := i.ComputeClaudeProcess(); p != nil {
		return p.SessionID
	}
	return ""
}

// ComputeClaudeProcess reads Claude Code's registry entry of the process in the instance's
// tmux pane, or returns nil if there is none or the program is not Claude Code. Safe to call
// from a background goroutine.
func (i *Instance) ComputeClaudeProcess() *claudestatus.Process {
	if i.claudeDir == "" || i.tmuxSession == nil {
		return nil
	}
	return claudestatus.ReadProcess(i.tmuxSession.PanePID())
}

// SetClaudeInfo sets what Claude Code last reported. Should be called from the main event
// loop to avoid data races with View.
func (i *Instance) SetClaudeInfo(info *claudestatus.Info) {
	i.claudeInfo = info
}

// GetClaudeInfo returns the model, effort and context use Claude Code last reported, or nil.
func (i *Instance) GetClaudeInfo() *claudestatus.Info {
	return i.claudeInfo
}

// SetDiffStats sets the diff statistics on the instance. Should be called from
// the main event loop to avoid data races with View.
func (i *Instance) SetDiffStats(stats *git.DiffStats) {
	i.diffStats = stats
}

// GetDiffStats returns the current git diff statistics
func (i *Instance) GetDiffStats() *git.DiffStats {
	return i.diffStats
}

// SendPrompt sends a prompt to the tmux session
func (i *Instance) SendPrompt(prompt string) error {
	if !i.started {
		return fmt.Errorf("instance not started")
	}
	if i.tmuxSession == nil {
		return fmt.Errorf("tmux session not initialized")
	}
	if err := i.tmuxSession.SendKeys(prompt); err != nil {
		return fmt.Errorf("error sending keys to tmux session: %w", err)
	}

	// Brief pause to prevent carriage return from being interpreted as newline
	time.Sleep(100 * time.Millisecond)
	if err := i.tmuxSession.TapEnter(); err != nil {
		return fmt.Errorf("error tapping enter: %w", err)
	}

	return nil
}

// PreviewFullHistory captures the entire tmux pane output including full scrollback history
func (i *Instance) PreviewFullHistory() (string, error) {
	if !i.started || i.Status == Paused {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContentWithOptions("-", "-")
}

// SetTmuxSession sets the tmux session for testing purposes
func (i *Instance) SetTmuxSession(session *tmux.TmuxSession) {
	i.tmuxSession = session
}

// SendKeys sends keys to the tmux session
func (i *Instance) SendKeys(keys string) error {
	if !i.started || i.Status == Paused {
		return fmt.Errorf("cannot send keys to instance that has not been started or is paused")
	}
	return i.tmuxSession.SendKeys(keys)
}
