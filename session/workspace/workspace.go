// Package workspace runs one claude-squad session across a directory that holds many git
// repositories but is not a git repository itself.
//
// Claude runs in the real directory, so it reads and searches every repository as usual.
// A Claude Code PreToolUse hook (see hook.go) gives a repository its own git worktree the
// first time Claude changes it, and from then on sends Claude to that worktree. The
// worktrees live in the workspace directory, and repos.json there lists them. The hook
// runs in its own process, so repos.json, not memory, is the source of truth.
package workspace

import (
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session/git"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// metaDirName holds claude-squad's own files inside the workspace directory.
	metaDirName      = ".claudesquad"
	configFileName   = "workspace.json"
	reposFileName    = "repos.json"
	lockFileName     = "lock"
	settingsFileName = "settings.json"
	promptFileName   = "system-prompt.md"
)

// Workspace is one session's set of per-repository worktrees.
type Workspace struct {
	// root is the directory holding the repositories, with symlinks resolved.
	root string
	// dir is where the session's worktrees and metadata live.
	dir         string
	sessionName string
	branchName  string
}

// Repo is a repository that has been given a worktree in the workspace.
type Repo struct {
	Name string `json:"name"`
	// BaseCommitSHA is the commit the worktree started from. Diffs are taken against it.
	BaseCommitSHA string `json:"base_commit_sha"`
}

// config is what the hook process needs to rebuild the Workspace; see Load.
type workspaceConfig struct {
	Root        string `json:"root"`
	SessionName string `json:"session_name"`
	BranchName  string `json:"branch_name"`
}

// HasRepos reports whether dir directly contains at least one git repository.
func HasRepos(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if isRepoDir(filepath.Join(dir, e.Name())) {
			return true
		}
	}
	return false
}

// SupportsProgram reports whether program can run in a workspace. Isolation depends on
// Claude Code hooks, so only Claude Code can.
func SupportsProgram(program string) bool {
	fields := strings.Fields(program)
	return len(fields) > 0 && filepath.Base(fields[0]) == "claude"
}

// isRepoDir reports whether path is a real directory (not a symlink) containing a git repository.
func isRepoDir(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func workspacesDir() (string, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "workspaces"), nil
}

// New creates a workspace for a session started in root. Nothing is written until Setup.
func New(root string, sessionName string) (*Workspace, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", root, err)
	}
	parent, err := workspacesDir()
	if err != nil {
		return nil, err
	}
	branchName := git.BranchNameFor(sessionName)
	dirName := fmt.Sprintf("%s_%x", strings.ReplaceAll(branchName, "/", "-"), time.Now().UnixNano())
	return &Workspace{
		root:        resolvedRoot,
		dir:         filepath.Join(parent, dirName),
		sessionName: sessionName,
		branchName:  branchName,
	}, nil
}

// FromStorage rebuilds a workspace from saved instance data.
func FromStorage(root string, dir string, sessionName string, branchName string) *Workspace {
	return &Workspace{root: root, dir: dir, sessionName: sessionName, branchName: branchName}
}

// Load rebuilds a workspace from the files Setup wrote into dir.
func Load(dir string) (*Workspace, error) {
	data, err := os.ReadFile(filepath.Join(dir, metaDirName, configFileName))
	if err != nil {
		return nil, fmt.Errorf("failed to read workspace config: %w", err)
	}
	var cfg workspaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse workspace config: %w", err)
	}
	return FromStorage(cfg.Root, dir, cfg.SessionName, cfg.BranchName), nil
}

func (w *Workspace) Root() string       { return w.root }
func (w *Workspace) Dir() string        { return w.dir }
func (w *Workspace) BranchName() string { return w.branchName }

func (w *Workspace) metaPath(name string) string {
	return filepath.Join(w.dir, metaDirName, name)
}

// WorktreePath returns where the named repository's worktree lives.
func (w *Workspace) WorktreePath(repo string) string {
	return filepath.Join(w.dir, repo)
}

// Setup writes the workspace's metadata and the Claude Code settings that install the
// isolation hook. csPath is the claude-squad binary the hook runs.
func (w *Workspace) Setup(csPath string) error {
	if err := os.MkdirAll(filepath.Join(w.dir, metaDirName), 0755); err != nil {
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}
	// Resolve now that the directory exists, so paths in messages match what tools report
	// (on macOS the temp directory, for one, lives behind a symlink).
	if resolved, err := filepath.EvalSymlinks(w.dir); err == nil {
		w.dir = resolved
	}

	cfg, err := json.MarshalIndent(workspaceConfig{
		Root:        w.root,
		SessionName: w.sessionName,
		BranchName:  w.branchName,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(w.metaPath(configFileName), cfg); err != nil {
		return err
	}
	if _, err := os.Stat(w.metaPath(reposFileName)); os.IsNotExist(err) {
		if err := w.writeRepos(nil); err != nil {
			return err
		}
	}

	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{map[string]any{
				"matcher": "Edit|Write|MultiEdit|NotebookEdit|Bash|Read|Grep|Glob",
				"hooks": []any{map[string]any{
					"type":    "command",
					"command": fmt.Sprintf("%s hook --workspace %s", shellQuote(csPath), shellQuote(w.dir)),
					// Creating a worktree for a large repository takes a few seconds.
					"timeout": 120,
				}},
			}},
		},
	}
	settingsJSON, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(w.metaPath(settingsFileName), settingsJSON); err != nil {
		return err
	}
	return writeFileAtomic(w.metaPath(promptFileName), []byte(w.systemPrompt()))
}

func (w *Workspace) systemPrompt() string {
	return fmt.Sprintf(`You are running in claude-squad session %q in %s, a directory holding many git repositories.

claude-squad isolates each repository the first time you change it: before your first edit (or shell command that changes files) in %s/<repo>, it creates a git worktree for that repository at %s/<repo> on branch %s and blocks the call with a message saying so. From then on, make every read, edit, build and git command for that repository in its worktree, and never change files under %s/<repo> directly.

When a tool call is blocked with such a message, follow it and redo the call in the worktree.
`, w.sessionName, w.root, w.root, w.dir, w.branchName, w.root)
}

// ClaudeArgs returns the extra command-line arguments that make Claude Code use the
// workspace: the isolation hook, write access to the worktrees, and instructions.
func (w *Workspace) ClaudeArgs() string {
	return fmt.Sprintf("--settings %s --add-dir %s --append-system-prompt-file %s",
		shellQuote(w.metaPath(settingsFileName)), shellQuote(w.dir), shellQuote(w.metaPath(promptFileName)))
}

// Repos returns the repositories that have a worktree in the workspace.
func (w *Workspace) Repos() ([]Repo, error) {
	data, err := os.ReadFile(w.metaPath(reposFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read workspace repos: %w", err)
	}
	var repos []Repo
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse workspace repos: %w", err)
	}
	return repos, nil
}

func (w *Workspace) writeRepos(repos []Repo) error {
	if repos == nil {
		repos = []Repo{}
	}
	data, err := json.MarshalIndent(repos, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(w.metaPath(reposFileName), data)
}

// worktree returns the GitWorktree for an isolated repository.
func (w *Workspace) worktree(r Repo) *git.GitWorktree {
	return git.NewGitWorktreeFromStorage(
		filepath.Join(w.root, r.Name), w.WorktreePath(r.Name), w.sessionName, w.branchName, r.BaseCommitSHA, false)
}

// Isolate gives the named repository a worktree, unless it already has one. It is safe to
// call from several processes at once.
func (w *Workspace) Isolate(name string) (created bool, err error) {
	if !isRepoDir(filepath.Join(w.root, name)) {
		return false, fmt.Errorf("%s is not a git repository", filepath.Join(w.root, name))
	}

	unlock, err := lockFile(w.metaPath(lockFileName))
	if err != nil {
		return false, fmt.Errorf("failed to lock workspace: %w", err)
	}
	defer unlock()

	repos, err := w.Repos()
	if err != nil {
		return false, err
	}
	for _, r := range repos {
		if r.Name == name {
			return false, nil
		}
	}

	wt := git.NewGitWorktreeAt(filepath.Join(w.root, name), w.WorktreePath(name), w.sessionName, w.branchName)
	// A worktree whose directory was deleted outside claude-squad stays registered and keeps
	// its branch checked out, so `git worktree add` would refuse the branch. Pruning only
	// drops registrations whose directory is gone.
	if err := wt.Prune(); err != nil {
		return false, err
	}
	if err := wt.Setup(); err != nil {
		return false, fmt.Errorf("failed to create worktree for %s: %w", name, err)
	}
	if err := w.writeRepos(append(repos, Repo{Name: name, BaseCommitSHA: wt.GetBaseCommitSHA()})); err != nil {
		return false, err
	}
	return true, nil
}

// Diff returns the diff of every isolated repository with changes, one entry per repository
// in DiffStats.Repos, plus the totals.
func (w *Workspace) Diff() *git.DiffStats {
	return w.diff((*git.GitWorktree).Diff)
}

// DiffNumstat is Diff without the diff content.
func (w *Workspace) DiffNumstat() *git.DiffStats {
	return w.diff((*git.GitWorktree).DiffNumstat)
}

func (w *Workspace) diff(diffFn func(*git.GitWorktree) *git.DiffStats) *git.DiffStats {
	stats := &git.DiffStats{}
	repos, err := w.Repos()
	if err != nil {
		stats.Error = err
		return stats
	}
	for _, r := range repos {
		s := diffFn(w.worktree(r))
		if s.Error != nil {
			// One broken worktree should not hide the others' changes.
			log.WarningLog.Printf("could not diff %s in workspace %s: %v", r.Name, w.dir, s.Error)
			continue
		}
		if s.IsEmpty() {
			continue
		}
		stats.Added += s.Added
		stats.Removed += s.Removed
		stats.Repos = append(stats.Repos, git.RepoDiff{Name: r.Name, Added: s.Added, Removed: s.Removed, Content: s.Content})
	}
	return stats
}

// CheckedOutRepos returns the repositories whose session branch is checked out in the
// real checkout. Their worktrees cannot be recreated, nor their branches deleted.
func (w *Workspace) CheckedOutRepos() ([]string, error) {
	repos, err := w.Repos()
	if err != nil {
		return nil, err
	}
	var checkedOut []string
	for _, r := range repos {
		ok, err := w.worktree(r).IsBranchCheckedOut()
		if err != nil {
			return nil, err
		}
		if ok {
			checkedOut = append(checkedOut, r.Name)
		}
	}
	return checkedOut, nil
}

// Pause commits each repository's changes to its session branch and removes the worktrees,
// keeping the branches.
func (w *Workspace) Pause(commitMessage string) error {
	repos, err := w.Repos()
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range repos {
		wt := w.worktree(r)
		if valid, err := wt.IsValidWorktree(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.Name, err))
			continue
		} else if !valid {
			_ = os.RemoveAll(wt.GetWorktreePath())
			if err := wt.Prune(); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", r.Name, err))
			}
			continue
		}
		if err := wt.CommitChanges(commitMessage); err != nil {
			// Removing the worktree now would lose the uncommitted changes.
			errs = append(errs, fmt.Errorf("%s: %w", r.Name, err))
			continue
		}
		if err := wt.Remove(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.Name, err))
			continue
		}
		if err := wt.Prune(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.Name, err))
		}
	}
	return errors.Join(errs...)
}

// Resume recreates the worktrees that Pause removed, from their session branches.
func (w *Workspace) Resume() error {
	repos, err := w.Repos()
	if err != nil {
		return err
	}
	for _, r := range repos {
		// Diffs keep using the base commit in repos.json, not the branch head Setup records.
		wt := w.worktree(r)
		if valid, err := wt.IsValidWorktree(); err == nil && valid {
			continue
		}
		if err := wt.Setup(); err != nil {
			return fmt.Errorf("failed to recreate worktree for %s: %w", r.Name, err)
		}
	}
	return nil
}

// Cleanup removes every worktree and session branch, then the workspace directory.
func (w *Workspace) Cleanup() error {
	repos, err := w.Repos()
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range repos {
		if err := w.worktree(r).Cleanup(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.Name, err))
		}
	}
	if len(errs) > 0 {
		// Keep the directory, and with it repos.json, so cleanup can be retried.
		return errors.Join(errs...)
	}

	parent, err := workspacesDir()
	if err != nil {
		return err
	}
	if !isWithin(w.dir, parent) {
		return fmt.Errorf("refusing to remove %s: not inside %s", w.dir, parent)
	}
	return os.RemoveAll(w.dir)
}

// CleanupAll removes every workspace, for `cs reset`.
func CleanupAll() error {
	parent, err := workspacesDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var errs []error
	for _, e := range entries {
		w, err := Load(filepath.Join(parent, e.Name()))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := w.Cleanup(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// isWithin reports whether path is strictly inside dir, comparing resolved paths.
func isWithin(path string, dir string) bool {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
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
