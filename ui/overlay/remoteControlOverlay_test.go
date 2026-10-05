package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

const testRemoteURL = "https://claude.ai/code/session_01PGK9UUiG9Uvb8nTr2TH9bP"

func TestRemoteControlOverlayShowsLinkAndQRCode(t *testing.T) {
	o := NewRemoteControlOverlay("fix the map", testRemoteURL, "Copied to the clipboard.", 60)
	require.True(t, o.HasQRCode())
	out := ansi.Strip(o.Render())
	require.Contains(t, out, testRemoteURL)
	require.Contains(t, out, "fix the map")
	require.Contains(t, out, "Copied to the clipboard.")
	require.Contains(t, out, "▀", "QR code")

	short := NewRemoteControlOverlay("fix the map", testRemoteURL, "", 20)
	require.False(t, short.HasQRCode(), "no room for the QR code")
	require.Contains(t, ansi.Strip(short.Render()), testRemoteURL, "the link always shows")
	require.LessOrEqual(t, strings.Count(short.Render(), "\n")+1, 20)
}

func TestRemoteControlOverlayKeys(t *testing.T) {
	for key, want := range map[string]RemoteChoice{"o": RemoteOpen, "d": RemoteDisconnect, "q": RemoteClose, "esc": RemoteClose} {
		o := NewRemoteControlOverlay("s", testRemoteURL, "", 60)
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		if key == "esc" {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		}
		require.True(t, o.HandleKeyPress(msg), "every key closes the overlay")
		require.Equal(t, want, o.Choice(), key)
	}
}
