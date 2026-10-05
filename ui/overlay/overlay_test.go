package overlay

import (
	"strings"
	"testing"

	"github.com/muesli/reflow/ansi"
	"github.com/stretchr/testify/require"
)

// Claude Code wraps file paths in OSC 8 hyperlinks. They are invisible, so they must not push
// a centered overlay to the right.
func TestPlaceOverlayCentersOverHyperlinks(t *testing.T) {
	link := "\x1b]8;id=abc;file:///Users/me/a/very/long/path/to/some/file.go\x1b\\file.go\x1b]8;;\x1b\\"
	bgLines := make([]string, 10)
	for i := range bgLines {
		bgLines[i] = strings.Repeat(".", 40)
	}
	bgLines[2] = "see " + link + strings.Repeat(".", 29)
	fg := "[box]"

	out := strings.Split(PlaceOverlay(0, 0, fg, strings.Join(bgLines, "\n"), false, true), "\n")
	require.Len(t, out, 10)
	row := out[4]
	require.Equal(t, 17, strings.Index(stripCSI(row), "[box]"), "centered in 40 columns: %q", row)
	require.Equal(t, 40, ansi.PrintableRuneWidth(row))
}

func stripCSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && ((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')):
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}
