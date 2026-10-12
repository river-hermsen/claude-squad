package overlay

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

func typeText(p *FolderPicker, text string) {
	for _, r := range text {
		p.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// newFolderTree makes $HOME/Projects/{claude-squad,foys-all/{core,web},.hidden} and returns
// the home directory.
func newFolderTree(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, dir := range []string{"Projects/claude-squad", "Projects/foys-all/core", "Projects/foys-all/web", "Projects/.hidden"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, dir), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, "Projects", "notes.md"), nil, 0o644))
	return home
}

func TestFolderPickerOffersRecentFoldersFirst(t *testing.T) {
	home := newFolderTree(t)
	foysAll := filepath.Join(home, "Projects", "foys-all")
	squad := filepath.Join(home, "Projects", "claude-squad")
	p := NewFolderPicker("Folder", foysAll, []string{foysAll, squad})
	require.Equal(t, []string{foysAll, squad}, p.candidates)

	require.False(t, p.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown}))
	require.True(t, p.HandleKeyPress(tea.KeyMsg{Type: tea.KeyEnter}))
	require.True(t, p.Submitted())
	require.Equal(t, squad, p.Value())
}

func TestFolderPickerCompletesPaths(t *testing.T) {
	home := newFolderTree(t)
	foysAll := filepath.Join(home, "Projects", "foys-all")
	p := NewFolderPicker("Folder", foysAll, nil)

	typeText(p, "~/Projects/")
	require.Equal(t, []string{
		filepath.Join(home, "Projects"),
		filepath.Join(home, "Projects", "claude-squad"),
		foysAll,
	}, p.candidates, "the folder itself, then its subdirectories, without files or hidden ones")

	typeText(p, "F")
	require.Equal(t, []string{foysAll}, p.candidates, "prefixes match regardless of case")

	p.HandleKeyPress(tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, "~/Projects/foys-all/", p.input.Value())
	require.Equal(t, []string{foysAll, filepath.Join(foysAll, "core"), filepath.Join(foysAll, "web")}, p.candidates)
}

func TestFolderPickerResolvesRelativePathsAgainstBaseDir(t *testing.T) {
	home := newFolderTree(t)
	foysAll := filepath.Join(home, "Projects", "foys-all")
	p := NewFolderPicker("Folder", foysAll, nil)

	typeText(p, "w")
	require.Equal(t, []string{filepath.Join(foysAll, "web")}, p.candidates)
	require.True(t, p.HandleKeyPress(tea.KeyMsg{Type: tea.KeyEnter}))
	require.Equal(t, filepath.Join(foysAll, "web"), p.Value())
}

func TestFolderPickerMatchesRecentFoldersAnywhere(t *testing.T) {
	home := newFolderTree(t)
	squad := filepath.Join(home, "Projects", "claude-squad")
	p := NewFolderPicker("Folder", filepath.Join(home, "Projects", "foys-all"), []string{squad})

	typeText(p, "squad")
	require.Equal(t, []string{squad}, p.candidates)
}

func TestFolderPickerRejectsMissingFolder(t *testing.T) {
	home := newFolderTree(t)
	p := NewFolderPicker("Folder", home, nil)

	typeText(p, "/does/not/exist")
	require.Empty(t, p.candidates)
	require.False(t, p.HandleKeyPress(tea.KeyMsg{Type: tea.KeyEnter}), "stays open")
	require.Contains(t, p.Render(), "no such folder")

	require.True(t, p.HandleKeyPress(tea.KeyMsg{Type: tea.KeyEsc}))
	require.False(t, p.Submitted())
}
