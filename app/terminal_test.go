package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectionKey(t *testing.T) {
	for _, tc := range []struct{ term, program, lcTerminal, want string }{
		{"xterm-ghostty", "", "", "Shift"},
		{"xterm-256color", "", "iTerm2", "Option"},
		{"xterm-256color", "Apple_Terminal", "", "Fn"},
		{"xterm-256color", "", "", "Fn (Terminal), Option (iTerm2) or Shift (Ghostty)"},
	} {
		t.Setenv("TERM", tc.term)
		t.Setenv("TERM_PROGRAM", tc.program)
		t.Setenv("LC_TERMINAL", tc.lcTerminal)
		require.Equal(t, tc.want, selectionKey(), tc)
	}
}
