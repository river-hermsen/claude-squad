package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// cs keeps its skill current, but leaves a claude-squad skill it did not write alone.
func TestInstallSkill(t *testing.T) {
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	path := filepath.Join(claudeDir, "skills", "claude-squad", "SKILL.md")

	require.NoError(t, installSkill("/home/me/.local/bin/cs"))
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(content), "name: claude-squad")
	require.Contains(t, string(content), "/home/me/.local/bin/cs new --title")

	require.NoError(t, installSkill("/usr/local/bin/cs"))
	content, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(content), "/usr/local/bin/cs new --title", "rewritten for the cs that runs now")

	require.NoError(t, os.WriteFile(path, []byte("---\nname: claude-squad\n---\nmy own\n"), 0644))
	require.NoError(t, installSkill("/home/me/.local/bin/cs"))
	content, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "---\nname: claude-squad\n---\nmy own\n", string(content))
}
