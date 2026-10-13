package claudestatus

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOlderVersion(t *testing.T) {
	require.True(t, OlderVersion("2.1.296", "2.1.297"))
	require.True(t, OlderVersion("2.1.296", "2.2.0"))
	require.True(t, OlderVersion("2.1.99", "2.1.100"), "numbers, not text")
	require.True(t, OlderVersion("2.1", "2.1.1"))
	require.False(t, OlderVersion("2.1.297", "2.1.297"))
	require.False(t, OlderVersion("2.1.297", "2.1.296"))
	require.False(t, OlderVersion("", "2.1.297"), "unknown")
	require.False(t, OlderVersion("2.1.296", ""))
}

func TestInstalledVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the claude binary")
	}
	dir := t.TempDir()

	// The native installer's layout: a link to versions/<version>.
	native := filepath.Join(dir, "versions", "2.1.297")
	require.NoError(t, os.MkdirAll(filepath.Dir(native), 0755))
	require.NoError(t, os.WriteFile(native, []byte("not run"), 0755))
	link := filepath.Join(dir, "bin", "claude")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0755))
	require.NoError(t, os.Symlink(native, link))
	require.Equal(t, "2.1.297", InstalledVersion(link+" --model haiku"))

	// Anything else is asked, until the binary changes.
	script := filepath.Join(dir, "other", "claude")
	require.NoError(t, os.MkdirAll(filepath.Dir(script), 0755))
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho '2.1.300 (Claude Code)'\n"), 0755))
	require.Equal(t, "2.1.300", InstalledVersion(script))
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho '2.1.301 (Claude Code)'\n"), 0755))
	require.NoError(t, os.Chtimes(script, time.Now(), time.Now().Add(time.Minute)))
	require.Equal(t, "2.1.301", InstalledVersion(script))

	require.Equal(t, "", InstalledVersion(filepath.Join(dir, "missing")))
	require.Equal(t, "", InstalledVersion(""))
}
