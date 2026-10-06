package session

import (
	"claude-squad/config"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// `cs new` hands the instances it starts to the TUI through the inbox.
func TestInboxRoundTrip(t *testing.T) {
	instance, _ := newInPlaceInstance(t, "from phone")
	instance.RemoteControl = true

	items, err := ReadInbox()
	require.NoError(t, err)
	require.Empty(t, items)

	require.NoError(t, AddToInbox(instance))
	items, err = ReadInbox()
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "from phone", items[0].Data.Title)
	require.True(t, items[0].Data.RemoteControl)
	require.Equal(t, instance.ToInstanceData().ClaudeDir, items[0].Data.ClaudeDir)

	// A file still being written is skipped.
	require.NoError(t, os.WriteFile(items[0].Path+".tmp", []byte("{"), 0644))
	items, err = ReadInbox()
	require.NoError(t, err)
	require.Len(t, items, 1)

	require.NoError(t, items[0].Remove())
	require.NoError(t, items[0].Remove(), "removing twice is fine")
	items, err = ReadInbox()
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestAddToInboxNeedsStartedInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	instance, err := NewInstance(InstanceOptions{Title: "new", Path: t.TempDir(), Program: "claude"})
	require.NoError(t, err)
	require.ErrorContains(t, AddToInbox(instance), "has not been started")
}

// Titles in use are those saved in state and those waiting in the inbox.
func TestStoredTitles(t *testing.T) {
	instance, _ := newInPlaceInstance(t, "waiting")
	require.NoError(t, AddToInbox(instance))

	state := config.LoadState()
	require.NoError(t, state.SaveInstances([]byte(`[{"title":"saved one"},{"title":"saved two"}]`)))
	require.Equal(t, []string{"saved one", "saved two", "waiting"}, StoredTitles(state))
	require.FileExists(t, filepath.Join(os.Getenv("HOME"), ".claude-squad", "state.json"))
}

func TestUniqueTitle(t *testing.T) {
	taken := map[string]bool{"fix the map": true, "fix the map 2": true}
	isTaken := func(title string) bool { return taken[title] }

	require.Equal(t, "fix the map 3", UniqueTitle("fix  the\tmap", isTaken))
	require.Equal(t, "session", UniqueTitle("  ", isTaken))
	long := UniqueTitle(strings.Repeat("abcdefghij", 5), isTaken)
	require.Equal(t, strings.Repeat("abcdefghij", 3)+"ab", long, "at most 32 characters")
}
