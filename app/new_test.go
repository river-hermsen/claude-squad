package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/ui"
	"context"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/stretchr/testify/require"
)

// newInboxTestHome returns a home with an empty list, saving to a state under a temporary HOME.
func newInboxTestHome(t *testing.T) *home {
	t.Setenv("HOME", t.TempDir())
	state := config.LoadState()
	storage, err := session.NewStorage(state)
	require.NoError(t, err)
	s := spinner.New()
	return &home{
		ctx:          context.Background(),
		state:        stateDefault,
		appConfig:    config.DefaultConfig(),
		storage:      storage,
		list:         ui.NewList(&s, false),
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
		errBox:       ui.NewErrBox(),
	}
}

// The TUI adds the sessions `cs new` started to its list and saves them; only then do they
// leave the inbox.
func TestAdoptInbox(t *testing.T) {
	m := newInboxTestHome(t)
	// Paused, so loading it does not reattach to a real tmux session.
	items := []session.InboxItem{
		{Path: t.TempDir() + "/a.json", Data: session.InstanceData{Title: "from phone", Path: t.TempDir(), Program: "claude", Status: session.Paused, RemoteControl: true}},
	}
	require.True(t, m.canAdoptInbox())
	require.True(t, m.adoptInbox(items))
	require.Len(t, m.list.GetInstances(), 1)
	inst := m.list.GetInstances()[0]
	require.Equal(t, "from phone", inst.Title)
	require.True(t, inst.RemoteControl)
	require.Equal(t, []string{"from phone"}, session.StoredTitles(config.LoadState()), "saved")

	require.True(t, m.adoptInbox(items), "the same session again only leaves the inbox")
	require.Len(t, m.list.GetInstances(), 1)
	require.False(t, m.adoptInbox(nil))
}

// While a new instance is being named, it is the list's last item, so nothing may come after it.
func TestNoInboxWhileNaming(t *testing.T) {
	m := newInboxTestHome(t)
	naming, err := session.NewInstance(session.InstanceOptions{Title: "", Path: t.TempDir(), Program: "claude"})
	require.NoError(t, err)
	m.list.AddInstance(naming)
	require.False(t, m.canAdoptInbox())

	m.list.Kill()
	m.state = stateNew
	require.False(t, m.canAdoptInbox())
}
