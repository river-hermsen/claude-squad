package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Setup creates a new worktree for the session
func (g *GitWorktree) Setup() error {
	// If this worktree uses a pre-existing branch, always set up from that branch
	// (it may exist locally or only on the remote).
	if g.isExistingBranch {
		return g.setupFromExistingBranch()
	}

	// Check if branch exists using git CLI (much faster than go-git PlainOpen)
	_, err := g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/heads/%s", g.branchName))
	if err == nil {
		return g.setupFromExistingBranch()
	}
	return g.setupNewWorktree()
}

// setupFromExistingBranch creates a worktree from an existing branch
func (g *GitWorktree) setupFromExistingBranch() error {
	// Directory already created in Setup(), skip duplicate creation

	// Clean up any existing worktree first
	_, _ = g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath) // Ignore error if worktree doesn't exist
	// If the directory is still there (orphaned, not registered with git), drop it so `git worktree add` won't fail.
	_ = os.RemoveAll(g.worktreePath)

	// Check if the local branch exists
	_, localErr := g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/heads/%s", g.branchName))
	if localErr != nil {
		// Local branch doesn't exist — check if remote tracking branch exists
		_, remoteErr := g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/remotes/origin/%s", g.branchName))
		if remoteErr != nil {
			return fmt.Errorf("branch %s not found locally or on remote", g.branchName)
		}
		// Create a local tracking branch via worktree add -b
		if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", g.branchName, g.worktreePath, fmt.Sprintf("origin/%s", g.branchName)); err != nil {
			return fmt.Errorf("failed to create worktree from remote branch %s: %w", g.branchName, err)
		}
		return g.recordBaseCommit()
	}

	// Create a new worktree from the existing local branch
	if _, err := g.runGitCommand(g.repoPath, "worktree", "add", g.worktreePath, g.branchName); err != nil {
		return fmt.Errorf("failed to create worktree from branch %s: %w", g.branchName, err)
	}

	return g.recordBaseCommit()
}

// recordBaseCommit stores the commit the session starts from. Diffs are computed
// against it, so leaving it unset makes every git diff invocation fail with an
// ambiguous argument error once the session is running.
func (g *GitWorktree) recordBaseCommit() error {
	output, err := g.runGitCommand(g.worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("failed to get base commit hash for branch %s: %w", g.branchName, err)
	}
	g.baseCommitSHA = strings.TrimSpace(output)
	return nil
}

// setupNewWorktree creates a new worktree from HEAD
func (g *GitWorktree) setupNewWorktree() error {
	// Clean up any existing worktree first
	_, _ = g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath) // Ignore error if worktree doesn't exist
	// If the directory is still there (orphaned, not registered with git), drop it so `git worktree add` won't fail.
	_ = os.RemoveAll(g.worktreePath)

	// Clean up any existing branch using git CLI (much faster than go-git PlainOpen)
	_, _ = g.runGitCommand(g.repoPath, "branch", "-D", g.branchName) // Ignore error if branch doesn't exist

	output, err := g.runGitCommand(g.repoPath, "rev-parse", "HEAD")
	if err != nil {
		if strings.Contains(err.Error(), "fatal: ambiguous argument 'HEAD'") ||
			strings.Contains(err.Error(), "fatal: not a valid object name") ||
			strings.Contains(err.Error(), "fatal: HEAD: not a valid object name") {
			return fmt.Errorf("this appears to be a brand new repository: please create an initial commit before creating an instance")
		}
		return fmt.Errorf("failed to get HEAD commit hash: %w", err)
	}
	headCommit := strings.TrimSpace(string(output))
	g.baseCommitSHA = headCommit

	// Create a new worktree from the HEAD commit
	// Otherwise, we'll inherit uncommitted changes from the previous worktree.
	// This way, we can start the worktree with a clean slate.
	// TODO: we might want to give an option to use main/master instead of the current branch.
	if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", g.branchName, g.worktreePath, headCommit); err != nil {
		return fmt.Errorf("failed to create worktree from commit %s: %w", headCommit, err)
	}

	return nil
}

// Cleanup removes the worktree and associated branch
func (g *GitWorktree) Cleanup() error {
	var errs []error

	// Check if worktree path exists before attempting removal
	if _, err := os.Stat(g.worktreePath); err == nil {
		// Remove the worktree using git command
		if _, err := g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath); err != nil {
			errs = append(errs, err)
		}
	} else if !os.IsNotExist(err) {
		// Only append error if it's not a "not exists" error
		errs = append(errs, fmt.Errorf("failed to check worktree path: %w", err))
	}

	// Delete the branch using git CLI, but skip if this is a pre-existing branch
	if !g.isExistingBranch {
		if _, err := g.runGitCommand(g.repoPath, "branch", "-D", g.branchName); err != nil {
			// Only log if it's not a "branch not found" error
			if !strings.Contains(err.Error(), "not found") {
				errs = append(errs, fmt.Errorf("failed to remove branch %s: %w", g.branchName, err))
			}
		}
	}

	// Prune the worktree to clean up any remaining references
	if err := g.Prune(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return g.combineErrors(errs)
	}

	return nil
}

// Remove removes the worktree but keeps the branch
func (g *GitWorktree) Remove() error {
	// Remove the worktree using git command
	if _, err := g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	return nil
}

// Prune removes all working tree administrative files and directories
func (g *GitWorktree) Prune() error {
	if _, err := g.runGitCommand(g.repoPath, "worktree", "prune"); err != nil {
		return fmt.Errorf("failed to prune worktrees: %w", err)
	}
	return nil
}

// Snapshot captures the session's current files: head is the commit the worktree has
// checked out, and snapshot a commit on top of it holding every file in the worktree,
// tracked or untracked (ignored files excepted). It changes nothing: the worktree, its
// index and its branch stay as they are. If the worktree is gone (the session is paused),
// both are the tip of the session's branch, which then holds all the session's work.
func (g *GitWorktree) Snapshot() (head string, snapshot string, err error) {
	if valid, _ := g.IsValidWorktree(); !valid {
		out, err := g.runGitCommand(g.repoPath, "rev-parse", "--verify", "refs/heads/"+g.branchName)
		if err != nil {
			return "", "", fmt.Errorf("failed to find branch %s: %w", g.branchName, err)
		}
		head = strings.TrimSpace(out)
		return head, head, nil
	}

	out, err := g.runGitCommand(g.worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("failed to find HEAD of %s: %w", g.worktreePath, err)
	}
	head = strings.TrimSpace(out)

	// Stage everything into a throwaway index, so the worktree's own index is untouched.
	index, err := os.CreateTemp("", "claudesquad-snapshot-index")
	if err != nil {
		return "", "", err
	}
	index.Close()
	defer os.Remove(index.Name())
	withIndex := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", g.worktreePath}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+index.Name())
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s failed: %s (%w)", args[0], output, err)
		}
		return strings.TrimSpace(string(output)), nil
	}
	if _, err := withIndex("read-tree", "HEAD"); err != nil {
		return "", "", err
	}
	if _, err := withIndex("add", "-A"); err != nil {
		return "", "", err
	}
	tree, err := withIndex("write-tree")
	if err != nil {
		return "", "", err
	}
	snapshot, err = withIndex("-c", "user.name=claude-squad", "-c", "user.email=claude-squad@localhost",
		"commit-tree", tree, "-p", head, "-m", "claude-squad fork snapshot")
	if err != nil {
		return "", "", err
	}
	return head, snapshot, nil
}

// SetupFrom creates the worktree on a new branch at head, with snapshot's files in it as
// uncommitted changes, so it looks like the worktree Snapshot was taken from. The base
// commit is kept as given, so diffs show the same changes as there.
func (g *GitWorktree) SetupFrom(head string, snapshot string) error {
	_, _ = g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath)
	_ = os.RemoveAll(g.worktreePath)
	_, _ = g.runGitCommand(g.repoPath, "branch", "-D", g.branchName)

	if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", g.branchName, g.worktreePath, head); err != nil {
		return fmt.Errorf("failed to create worktree from commit %s: %w", head, err)
	}
	if snapshot == head {
		return nil
	}
	// Check out the snapshot's files, then point the index back at head: the files stay,
	// as changes on top of head.
	if _, err := g.runGitCommand(g.worktreePath, "read-tree", "-u", "--reset", snapshot); err != nil {
		return fmt.Errorf("failed to copy files into %s: %w", g.worktreePath, err)
	}
	if _, err := g.runGitCommand(g.worktreePath, "reset", "-q"); err != nil {
		return fmt.Errorf("failed to reset index of %s: %w", g.worktreePath, err)
	}
	return nil
}
