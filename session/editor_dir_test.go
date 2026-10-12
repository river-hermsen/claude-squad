package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// EditorDir is the worktree to open in an editor: the one repository's, or with several the
// workspace that holds them. Sessions in place have only Path.
func TestEditorDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	load := func(data InstanceData) *Instance {
		data.Title, data.Program, data.Status = "edit", "claude", Paused
		inst, err := FromInstanceData(data)
		require.NoError(t, err)
		return inst
	}

	inPlace := load(InstanceData{Path: "/src/notes"})
	dir, err := inPlace.EditorDir()
	require.NoError(t, err)
	require.Equal(t, "/src/notes", dir, "a paused session in place still has its directory")

	singleDir := t.TempDir()
	single := load(InstanceData{Path: "/src/app", Workspace: &WorkspaceData{Root: "/src", Dir: singleDir, BranchName: "me/edit", SingleRepo: "app"}})
	_, err = single.EditorDir()
	require.ErrorContains(t, err, "r resumes it", "the worktree is gone")
	require.NoError(t, os.Mkdir(filepath.Join(singleDir, "app"), 0o755))
	dir, err = single.EditorDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(singleDir, "app"), dir, "paused or not")

	wsDir := t.TempDir()
	multi := load(InstanceData{Path: "/src/foys-all", Workspace: &WorkspaceData{Root: "/src/foys-all", Dir: wsDir, BranchName: "me/edit"}})
	multi.SetStatus(Running)
	_, err = multi.EditorDir()
	require.ErrorContains(t, err, "no worktree yet")

	// The worktrees on disk count, not repos.json: the agent may have made one itself.
	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, ".claudesquad"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, ".claudesquad", "repos.json"),
		[]byte(`[{"name":"foys-backend","base_commit_sha":"abc"}]`), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(wsDir, "foys-backend"), 0o755))
	dir, err = multi.EditorDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(wsDir, "foys-backend"), dir)

	require.NoError(t, os.Mkdir(filepath.Join(wsDir, "foys-command"), 0o755))
	dir, err = multi.EditorDir()
	require.NoError(t, err)
	require.Equal(t, wsDir, dir)
}

// Claude Code's picker of conversations to resume lists only the current worktree's until
// Ctrl+W; its hint, which it wraps itself, says which it shows.
func TestPickerOffersAllWorktrees(t *testing.T) {
	limited := "  Resume session\n    repo · wt\n    Ctrl+A to show all projects · Ctrl+B to only show current branch · Ctrl+W to show all\n    worktrees · Space to preview · Type to search · Esc to cancel\n"
	require.True(t, pickerOffersAllWorktrees(limited))
	widened := "    Ctrl+A to show all projects · Ctrl+B to only show current branch · Ctrl+W to only show current worktree · Type to search\n"
	require.False(t, pickerOffersAllWorktrees(widened))
	require.False(t, pickerOffersAllWorktrees("❯ \n"))
}
