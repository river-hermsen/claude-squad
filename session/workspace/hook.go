package workspace

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// hookInput is the part of Claude Code's PreToolUse hook input that the hook uses.
type hookInput struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
	Cwd       string         `json:"cwd"`
}

type hookOutput struct {
	HookSpecificOutput hookDecision `json:"hookSpecificOutput"`
}

type hookDecision struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// RunHook handles one PreToolUse call from Claude Code. It reads the tool call from in and,
// when the call must go to a worktree instead, isolates the repository and writes a deny
// decision explaining where to redo it. Writing nothing lets the call through.
func RunHook(dir string, in io.Reader, out io.Writer) error {
	w, err := Load(dir)
	if err != nil {
		return err
	}
	var input hookInput
	if err := json.NewDecoder(in).Decode(&input); err != nil {
		return fmt.Errorf("failed to parse hook input: %w", err)
	}
	reason := w.decide(input)
	if reason == "" {
		return nil
	}
	return json.NewEncoder(out).Encode(hookOutput{HookSpecificOutput: hookDecision{
		HookEventName:            "PreToolUse",
		PermissionDecision:       "deny",
		PermissionDecisionReason: reason,
	}})
}

// decide returns why the tool call must be redone in a worktree, or "" to let it through.
func (w *Workspace) decide(in hookInput) string {
	repos, err := w.Repos()
	if err != nil {
		return fmt.Sprintf("claude-squad could not read its workspace (%v). Do not change files under %s until this is fixed.", err, w.root)
	}
	isolated := make(map[string]bool, len(repos))
	for _, r := range repos {
		isolated[r.Name] = true
	}

	switch in.ToolName {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		path := stringField(in.ToolInput, "file_path")
		if path == "" {
			path = stringField(in.ToolInput, "notebook_path")
		}
		repo, rel := w.repoOf(resolvePath(path, in.Cwd))
		if repo == "" {
			return ""
		}
		created, err := w.Isolate(repo)
		if err != nil {
			return w.isolateFailed(repo, err)
		}
		target := filepath.Join(w.WorktreePath(repo), rel)
		if created {
			return fmt.Sprintf("%s is now isolated for this claude-squad session in a git worktree at %s (branch %s). "+
				"This change was not applied. Read %s and make the change there. "+
				"From now on make every read, edit, build and git command for %s in %s, and never change files under %s.",
				repo, w.WorktreePath(repo), w.branchName, target, repo, w.WorktreePath(repo), filepath.Join(w.root, repo))
		}
		return fmt.Sprintf("%s is isolated for this claude-squad session in %s. This change was not applied: make it in %s instead, "+
			"and never change files under %s.", repo, w.WorktreePath(repo), target, filepath.Join(w.root, repo))

	case "Read", "Grep", "Glob":
		// Only stop reads of a repository that has a worktree: its real checkout no longer
		// shows the session's changes. Everything else reads the real checkouts as usual.
		key := "path"
		if in.ToolName == "Read" {
			key = "file_path"
		}
		path := stringField(in.ToolInput, key)
		if path == "" {
			return ""
		}
		repo, rel := w.repoOf(resolvePath(path, in.Cwd))
		if repo == "" || !isolated[repo] {
			return ""
		}
		return fmt.Sprintf("%s is isolated for this claude-squad session; %s does not have this session's changes. "+
			"Use %s instead.", repo, filepath.Join(w.root, repo), filepath.Join(w.WorktreePath(repo), rel))

	case "Bash":
		command := stringField(in.ToolInput, "command")
		targets := w.bashTargets(command, in.Cwd)
		if len(targets) == 0 {
			return ""
		}
		risky := isRiskyCommand(command)
		var redirect []string
		for _, repo := range targets {
			if isolated[repo] {
				redirect = append(redirect, repo)
				continue
			}
			if risky {
				if _, err := w.Isolate(repo); err != nil {
					return w.isolateFailed(repo, err)
				}
				redirect = append(redirect, repo)
			}
		}
		if len(redirect) == 0 {
			return ""
		}
		var places []string
		for _, repo := range redirect {
			places = append(places, fmt.Sprintf("%s -> %s", repo, w.WorktreePath(repo)))
		}
		return fmt.Sprintf("This command was not run. In this claude-squad session these repositories are isolated in git worktrees "+
			"(on branch %s): %s. Run the command in the worktree instead (for example cd into it first), "+
			"and never change files in the original checkouts under %s.",
			w.branchName, strings.Join(places, ", "), w.root)
	}
	return ""
}

func (w *Workspace) isolateFailed(repo string, err error) string {
	return fmt.Sprintf("claude-squad could not create a worktree for %s (%v). Do not change files under %s; "+
		"tell the user isolation failed.", repo, err, filepath.Join(w.root, repo))
}

// repoOf returns the repository under root that contains path, and path relative to that
// repository. It returns "" for paths outside root's repositories.
func (w *Workspace) repoOf(path string) (repo string, rel string) {
	if path == "" {
		return "", ""
	}
	r, err := filepath.Rel(w.root, path)
	if err != nil || r == "." || strings.HasPrefix(r, "..") {
		return "", ""
	}
	parts := strings.SplitN(r, string(filepath.Separator), 2)
	if !isRepoDir(filepath.Join(w.root, parts[0])) {
		return "", ""
	}
	if len(parts) == 2 {
		rel = parts[1]
	}
	return parts[0], rel
}

// bashTargets returns the repositories a shell command works in: the one its working
// directory is in, and any it names. Over-reporting only costs an unneeded worktree.
func (w *Workspace) bashTargets(command string, cwd string) []string {
	found := map[string]bool{}
	if repo, _ := w.repoOf(resolvePath(cwd, "")); repo != "" {
		found[repo] = true
	}

	// Names inside worktree paths refer to the worktrees, which are fine to use.
	stripped := regexp.MustCompile(regexp.QuoteMeta(w.dir)+`[^\s'";|&)]*`).ReplaceAllString(command, "")
	entries, _ := os.ReadDir(w.root)
	for _, e := range entries {
		name := e.Name()
		if found[name] || !isRepoDir(filepath.Join(w.root, name)) {
			continue
		}
		if mentionsPathSegment(stripped, name) {
			found[name] = true
		}
	}

	targets := make([]string, 0, len(found))
	for repo := range found {
		targets = append(targets, repo)
	}
	sort.Strings(targets)
	return targets
}

// mentionsPathSegment reports whether name appears in command as a whole path segment,
// as in `cd name`, `name/src` or `../name`.
func mentionsPathSegment(command string, name string) bool {
	re := regexp.MustCompile(`(^|[\s'"=:(/])` + regexp.QuoteMeta(name) + `($|[\s'";|&)/])`)
	return re.MatchString(command)
}

var (
	riskyPatterns = []*regexp.Regexp{
		// git subcommands that change the working tree, index or branches, after any
		// global options such as `-C <path>`.
		regexp.MustCompile(`\bgit((\s+-[Cc]\s+\S+)|(\s+--?[\w-]+(=\S+)?))*\s+(commit|add|rm|mv|checkout|switch|restore|reset|stash|merge|rebase|cherry-pick|revert|apply|am|pull|clean)\b`),
		// In-place editors.
		regexp.MustCompile(`\b(sed|perl)\s+(-[a-zA-Z]*i|--in-place)`),
		// File operations.
		regexp.MustCompile(`(^|[\s;&|(])(rm|mv|cp|touch|mkdir|rmdir|ln|chmod|truncate|tee|patch|unzip|tar)\s`),
		// Package managers, generators and formatters.
		regexp.MustCompile(`\b(npm|pnpm|yarn|bun)\s+(install|i|add|remove|rm|uninstall|update|up|upgrade|dedupe|link|unlink|version|init|ci)\b`),
		regexp.MustCompile(`\b(npm|pnpm|yarn|bun)\s+(run\s+)?(format|lint:fix|fix)\b`),
		regexp.MustCompile(`\bdotnet\s+(ef|new|add|remove|sln|format|tool)\b`),
		regexp.MustCompile(`\b(pub\s+(add|remove|get|upgrade|downgrade)|build_runner|dart\s+(format|fix))\b`),
		regexp.MustCompile(`\s--(write|fix)\b`),
	}
	// Redirects that write nothing to the repository.
	harmlessRedirect = regexp.MustCompile(`\d*>&\d+|&?\d*>>?\s*/dev/null`)
	// An output redirect to a file, but not `=>` or `->`.
	fileRedirect = regexp.MustCompile(`(^|[^=\-<>])>>?\s*[^\s&|>=]`)
)

// isRiskyCommand reports whether a shell command looks like it changes files. It is a
// heuristic: commands it misses run against the real checkout.
func isRiskyCommand(command string) bool {
	for _, re := range riskyPatterns {
		if re.MatchString(command) {
			return true
		}
	}
	return fileRedirect.MatchString(harmlessRedirect.ReplaceAllString(command, ""))
}

// resolvePath makes path absolute against cwd and resolves symlinks in the longest prefix
// that exists, so files that do not exist yet still resolve.
func resolvePath(path string, cwd string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	path = filepath.Clean(path)

	existing, rest := path, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return path
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return path
	}
	return filepath.Join(resolved, rest)
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}
