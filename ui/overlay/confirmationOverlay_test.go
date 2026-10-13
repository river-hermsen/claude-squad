package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

func TestConfirmationOverlayOffersSecondAction(t *testing.T) {
	a := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}

	plain := NewConfirmationOverlay("Kill?")
	require.False(t, plain.HandleKeyPress(a), "no second action")

	c := NewConfirmationOverlay("Restart?")
	c.SetWidth(100)
	picked := ""
	c.OnConfirm = func() { picked = "confirm" }
	c.SetAlt("a", "restart all 3", func() { picked = "all" })
	require.Contains(t, c.Render(), "a to restart all 3, ")
	require.True(t, c.HandleKeyPress(a))
	require.Equal(t, "all", picked)
}
