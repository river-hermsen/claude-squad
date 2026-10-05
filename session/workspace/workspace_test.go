package workspace

import (
	"bytes"
	"claude-squad/log"
	"claude-squad/session/git"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	log.Initialize(false)
	defer log.Close()
	os.Exit(m.Run())
}

// newTestWorkspace creates a root directory holding the named git repositories plus a
// plain plans/ directory, and a workspace for it. HOME points at a temp directory so
// nothing touches the real ~/.claude-squad.
func newTestWorkspace(t *testing.T, repos ...string) *Workspace {
	t.Setenv("HOME", t.TempDir())
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "test")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "test@example.com")
	}

	root := t.TempDir()
	for _, name := range repos {
		dir := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "app.cs"), []byte("class App {}\n"), 0644))
		mustGit(t, dir, "init", "-q", "-b", "main")
		mustGit(t, dir, "add", ".")
		mustGit(t, dir, "commit", "-q", "-m", "init")
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "plans"), 0755))

	w, err := New(root, "my session")
	require.NoError(t, err)
	require.NoError(t, w.Setup("/usr/local/bin/cs"))
	return w
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func decide(w *Workspace, tool string, cwd string, input map[string]any) string {
	return w.decide(hookInput{ToolName: tool, ToolInput: input, Cwd: cwd})
}

func repoNames(t *testing.T, w *Workspace) []string {
	repos, err := w.Repos()
	require.NoError(t, err)
	var names []string
	for _, r := range repos {
		names = append(names, r.Name)
	}
	return names
}

func TestHasReposAndSupportsProgram(t *testing.T) {
	w := newTestWorkspace(t, "repoA")
	require.True(t, HasRepos(w.Root()))
	require.False(t, HasRepos(filepath.Join(w.Root(), "plans")))

	require.True(t, SupportsProgram("claude"))
	require.True(t, SupportsProgram("/Users/me/.local/bin/claude --model haiku"))
	require.False(t, SupportsProgram("codex"))
	require.False(t, SupportsProgram("aider --model claude"))
}

// The first edit in a repository creates its worktree and sends Claude there; the real
// checkout is not touched.
func TestEditInRepoIsolatesItAndRedirects(t *testing.T) {
	w := newTestWorkspace(t, "repoA", "repoB")
	head := mustGit(t, filepath.Join(w.Root(), "repoA"), "rev-parse", "HEAD")

	reason := decide(w, "Edit", w.Root(), map[string]any{"file_path": filepath.Join(w.Root(), "repoA", "src", "app.cs")})

	require.Contains(t, reason, "repoA is now isolated")
	require.Contains(t, reason, filepath.Join(w.WorktreePath("repoA"), "src", "app.cs"))
	require.FileExists(t, filepath.Join(w.WorktreePath("repoA"), "src", "app.cs"))
	require.Equal(t, []string{"repoA"}, repoNames(t, w), "only the edited repository gets a worktree")

	repos, _ := w.Repos()
	require.Equal(t, head, repos[0].BaseCommitSHA)
	require.Equal(t, w.BranchName(), mustGit(t, w.WorktreePath("repoA"), "branch", "--show-current"))
	require.Empty(t, mustGit(t, filepath.Join(w.Root(), "repoA"), "status", "--porcelain"))

	// A second edit to the real checkout is redirected again, without a new worktree.
	reason = decide(w, "Write", w.Root(), map[string]any{"file_path": filepath.Join(w.Root(), "repoA", "README.md")})
	require.Contains(t, reason, "repoA is isolated")
	require.Equal(t, []string{"repoA"}, repoNames(t, w))
}

func TestEditsThatNeedNoWorktreeAreAllowed(t *testing.T) {
	w := newTestWorkspace(t, "repoA")
	require.NotEmpty(t, decide(w, "Edit", w.Root(), map[string]any{"file_path": filepath.Join(w.Root(), "repoA", "README.md")}))

	for name, path := range map[string]string{
		"in the worktree":       filepath.Join(w.WorktreePath("repoA"), "README.md"),
		"outside repositories":  filepath.Join(w.Root(), "plans", "plan.md"),
		"outside the root":      filepath.Join(t.TempDir(), "other.txt"),
		"the root itself":       filepath.Join(w.Root(), "OVERVIEW.md"),
		"new file in worktree":  filepath.Join(w.WorktreePath("repoA"), "new", "file.txt"),
		"relative, not in repo": "plans/other.md",
	} {
		require.Empty(t, decide(w, "Write", w.Root(), map[string]any{"file_path": path}), name)
	}
}

func TestNewFileAndRelativePathsResolveToTheirRepository(t *testing.T) {
	w := newTestWorkspace(t, "repoA", "repoB")

	reason := decide(w, "Write", w.Root(), map[string]any{"file_path": filepath.Join(w.Root(), "repoB", "new", "dir", "file.txt")})
	require.Contains(t, reason, filepath.Join(w.WorktreePath("repoB"), "new", "dir", "file.txt"))

	reason = decide(w, "Edit", w.Root(), map[string]any{"file_path": "repoA/README.md"})
	require.Contains(t, reason, filepath.Join(w.WorktreePath("repoA"), "README.md"))
	require.ElementsMatch(t, []string{"repoA", "repoB"}, repoNames(t, w))
}

// Reads go to the real checkouts, except for isolated repositories, whose real checkout
// no longer shows the session's changes.
func TestReadsOfIsolatedRepositoriesAreRedirected(t *testing.T) {
	w := newTestWorkspace(t, "repoA", "repoB")
	_, err := w.Isolate("repoA")
	require.NoError(t, err)

	require.Contains(t, decide(w, "Read", w.Root(), map[string]any{"file_path": filepath.Join(w.Root(), "repoA", "README.md")}),
		filepath.Join(w.WorktreePath("repoA"), "README.md"))
	require.NotEmpty(t, decide(w, "Grep", w.Root(), map[string]any{"pattern": "x", "path": filepath.Join(w.Root(), "repoA", "src")}))

	require.Empty(t, decide(w, "Read", w.Root(), map[string]any{"file_path": filepath.Join(w.Root(), "repoB", "README.md")}))
	require.Empty(t, decide(w, "Grep", w.Root(), map[string]any{"pattern": "x", "path": w.Root()}))
	require.Empty(t, decide(w, "Glob", w.Root(), map[string]any{"pattern": "**/*.cs"}))
	require.Equal(t, []string{"repoA"}, repoNames(t, w), "reads never create worktrees")
}

func TestBashIsolatesRepositoriesOnlyForCommandsThatChangeFiles(t *testing.T) {
	w := newTestWorkspace(t, "repoA", "repoB", "repoC")
	repoA := filepath.Join(w.Root(), "repoA")

	require.Empty(t, decide(w, "Bash", repoA, map[string]any{"command": "git log --oneline -5"}))
	require.Empty(t, decide(w, "Bash", w.Root(), map[string]any{"command": "dotnet build repoB/App.sln"}))
	require.Empty(t, decide(w, "Bash", w.Root(), map[string]any{"command": "echo note > plans/note.md"}))
	require.Empty(t, repoNames(t, w))

	reason := decide(w, "Bash", repoA, map[string]any{"command": "git commit -am 'fix'"})
	require.Contains(t, reason, "repoA -> "+w.WorktreePath("repoA"))

	reason = decide(w, "Bash", w.Root(), map[string]any{"command": "cd repoB && sed -i '' 's/a/b/' README.md"})
	require.Contains(t, reason, "repoB -> "+w.WorktreePath("repoB"))
	require.ElementsMatch(t, []string{"repoA", "repoB"}, repoNames(t, w))

	// Once isolated, any command against the real checkout is redirected, even a read.
	require.Contains(t, decide(w, "Bash", repoA, map[string]any{"command": "dotnet build"}), "repoA ->")

	// Commands in a worktree, or naming one, are fine.
	require.Empty(t, decide(w, "Bash", w.WorktreePath("repoA"), map[string]any{"command": "git commit -am 'fix'"}))
	require.Empty(t, decide(w, "Bash", w.Root(), map[string]any{"command": "git -C " + w.WorktreePath("repoB") + " add ."}))
	require.ElementsMatch(t, []string{"repoA", "repoB"}, repoNames(t, w), "repoC was never touched")
}

func TestIsRiskyCommand(t *testing.T) {
	risky := []string{
		"git commit -m 'x'",
		"git -C ../repoA checkout -b feature/x",
		"git --no-pager stash",
		"git add -A && git status",
		"sed -i '' 's/a/b/' file.cs",
		"perl -pi -e 's/a/b/' file.cs",
		"rm -rf bin",
		"cd src && mv a.cs b.cs",
		"cat > appsettings.json <<EOF",
		"echo x >> README.md",
		"pnpm install",
		"npm run format",
		"yarn add lodash",
		"dotnet ef migrations add Init",
		"dotnet new classlib -n Foo",
		"fvm flutter pub get",
		"dart run build_runner build",
		"npx prettier --write .",
		"npx eslint --fix src",
	}
	safe := []string{
		"git log --oneline --format='%h %s'",
		"git status",
		"git diff HEAD~1",
		"git log --grep commit",
		"git show HEAD:src/app.cs",
		"git fetch origin",
		"grep -rn 'x => x.Id' src",
		"ls -la 2>/dev/null",
		"dotnet build 2>&1 | tail -20",
		"dotnet test --filter Foo > /dev/null",
		"cat README.md",
		"pnpm test",
		"rg -n 'a->b' src",
	}
	for _, c := range risky {
		require.True(t, isRiskyCommand(c), "should be risky: %s", c)
	}
	for _, c := range safe {
		require.False(t, isRiskyCommand(c), "should be safe: %s", c)
	}
}

func TestDiffCombinesChangedRepositories(t *testing.T) {
	w := newTestWorkspace(t, "repoA", "repoB", "repoC")
	for _, r := range []string{"repoA", "repoB", "repoC"} {
		_, err := w.Isolate(r)
		require.NoError(t, err)
	}
	require.NoError(t, os.WriteFile(filepath.Join(w.WorktreePath("repoA"), "README.md"), []byte("goodbye\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(w.WorktreePath("repoB"), "src", "new.cs"), []byte("class New {}\n"), 0644))

	stats := w.Diff()
	require.NoError(t, stats.Error)
	require.Equal(t, []string{"repoA", "repoB"}, stats.RepoNames(), "repoC has no changes")
	require.Equal(t, 2, stats.Added)
	require.Equal(t, 1, stats.Removed)
	require.Equal(t, git.RepoDiff{Name: "repoA", Added: 1, Removed: 1, Content: stats.Repos[0].Content}, stats.Repos[0])
	require.Contains(t, stats.Repos[0].Content, "a/README.md")
	require.Contains(t, stats.Repos[1].Content, "b/src/new.cs")
	require.NotContains(t, stats.Repos[1].Content, "README.md", "each repository's diff holds only its own changes")

	numstat := w.DiffNumstat()
	require.Equal(t, stats.RepoNames(), numstat.RepoNames())
	require.Equal(t, stats.Added, numstat.Added)
	require.Empty(t, numstat.Repos[0].Content)

	// Committed changes still count: the diff is against the base commit.
	mustGit(t, w.WorktreePath("repoA"), "commit", "-qam", "change")
	require.Equal(t, []string{"repoA", "repoB"}, w.DiffNumstat().RepoNames())
}

func TestPauseKeepsBranchesAndResumeRestoresWorktrees(t *testing.T) {
	w := newTestWorkspace(t, "repoA")
	_, err := w.Isolate("repoA")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(w.WorktreePath("repoA"), "README.md"), []byte("goodbye\n"), 0644))

	require.NoError(t, w.Pause("pause"))
	require.NoDirExists(t, w.WorktreePath("repoA"))
	require.Equal(t, "goodbye", mustGit(t, filepath.Join(w.Root(), "repoA"), "show", w.BranchName()+":README.md"))

	require.NoError(t, w.Resume())
	require.FileExists(t, filepath.Join(w.WorktreePath("repoA"), "README.md"))
	require.Equal(t, []string{"repoA"}, w.DiffNumstat().RepoNames(), "diff is still against the original base")
}

func TestCleanupRemovesWorktreesBranchesAndDirectory(t *testing.T) {
	w := newTestWorkspace(t, "repoA", "repoB")
	_, err := w.Isolate("repoA")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(w.WorktreePath("repoA"), "README.md"), []byte("goodbye\n"), 0644))
	repoA := filepath.Join(w.Root(), "repoA")

	require.NoError(t, w.Cleanup())

	require.NoDirExists(t, w.Dir())
	require.Empty(t, mustGit(t, repoA, "branch", "--list", w.BranchName()))
	require.Equal(t, 1, len(strings.Split(mustGit(t, repoA, "worktree", "list"), "\n")))
	content, err := os.ReadFile(filepath.Join(repoA, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "hello\n", string(content), "the real checkout is untouched")
}

func TestCleanupAllRemovesEveryWorkspace(t *testing.T) {
	w := newTestWorkspace(t, "repoA")
	_, err := w.Isolate("repoA")
	require.NoError(t, err)

	require.NoError(t, CleanupAll())
	require.NoDirExists(t, w.Dir())
	require.Empty(t, mustGit(t, filepath.Join(w.Root(), "repoA"), "branch", "--list", w.BranchName()))
}

// Claude can make several edits in parallel; each hook runs in its own process.
func TestConcurrentIsolateCreatesOneWorktree(t *testing.T) {
	w := newTestWorkspace(t, "repoA")
	var wg sync.WaitGroup
	created := make([]bool, 5)
	for i := range created {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := Load(w.Dir())
			require.NoError(t, err)
			created[i], err = c.Isolate("repoA")
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()

	n := 0
	for _, c := range created {
		if c {
			n++
		}
	}
	require.Equal(t, 1, n)
	require.Equal(t, []string{"repoA"}, repoNames(t, w))
}

func TestRunHookWritesDenyDecision(t *testing.T) {
	w := newTestWorkspace(t, "repoA")
	input := `{"tool_name":"Edit","cwd":"` + w.Root() + `","tool_input":{"file_path":"` + filepath.Join(w.Root(), "repoA", "README.md") + `"}}`

	var out bytes.Buffer
	require.NoError(t, RunHook(w.Dir(), strings.NewReader(input), &out))

	var decoded hookOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	require.Equal(t, "PreToolUse", decoded.HookSpecificOutput.HookEventName)
	require.Equal(t, "deny", decoded.HookSpecificOutput.PermissionDecision)
	require.Contains(t, decoded.HookSpecificOutput.PermissionDecisionReason, w.WorktreePath("repoA"))

	// Allowed calls produce no output at all.
	out.Reset()
	input = `{"tool_name":"Edit","cwd":"` + w.Root() + `","tool_input":{"file_path":"` + filepath.Join(w.Root(), "plans", "a.md") + `"}}`
	require.NoError(t, RunHook(w.Dir(), strings.NewReader(input), &out))
	require.Empty(t, out.String())
}

func TestClaudeArgsPointAtWorkspaceFiles(t *testing.T) {
	w := newTestWorkspace(t, "repoA")
	args := w.ClaudeArgs()
	require.Contains(t, args, "--settings '"+filepath.Join(w.Dir(), metaDirName, settingsFileName)+"'")
	require.Contains(t, args, "--add-dir '"+w.Dir()+"'")

	data, err := os.ReadFile(filepath.Join(w.Dir(), metaDirName, settingsFileName))
	require.NoError(t, err)
	require.Contains(t, string(data), `'/usr/local/bin/cs' hook --workspace '`+w.Dir()+`'`)
}

// A workspace directory deleted outside claude-squad leaves its worktrees registered in the
// repositories, with the session branch checked out. A new session with the same name must
// still be able to isolate them.
func TestIsolateRecoversFromDeletedWorkspace(t *testing.T) {
	w := newTestWorkspace(t, "repoA")
	_, err := w.Isolate("repoA")
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(w.Dir()))

	again, err := New(w.Root(), "my session")
	require.NoError(t, err)
	require.NoError(t, again.Setup("/usr/local/bin/cs"))
	created, err := again.Isolate("repoA")
	require.NoError(t, err)
	require.True(t, created)
	require.FileExists(t, filepath.Join(again.WorktreePath("repoA"), "README.md"))
}
