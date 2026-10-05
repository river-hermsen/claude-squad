package overlay

import (
	"regexp"
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

// Text after a reset, bold text and colors set together with attributes all used to keep their
// look behind a dialog. Everything must turn the same gray.
func TestFadeLineGraysEverything(t *testing.T) {
	line := "plain \x1b[1mbold\x1b[0m after reset \x1b[1;38;2;255;0;0mred bold\x1b[m end"
	require.Equal(t,
		fadedText+"plain "+fadedText+"bold"+fadedText+" after reset "+fadedText+"red bold"+fadedText+" end\x1b[0m",
		fadeLine(line))

	// 38;5;0 is black text, not a reset: the background stays until 49.
	line = "\x1b[48;2;40;40;40m selected \x1b[38;5;0mstill\x1b[49m none \x1b[2Kcleared"
	require.Equal(t,
		fadedText+fadedBackground+" selected "+fadedBackground+"still"+fadedText+" none \x1b[2Kcleared\x1b[0m",
		fadeLine(line))
}

func TestPlaceOverlayFadesWholeBackground(t *testing.T) {
	bgLines := make([]string, 5)
	for i := range bgLines {
		bgLines[i] = "\x1b[1mBold\x1b[0m default \x1b[38;2;255;255;255mwhite\x1b[0m" + strings.Repeat(".", 20)
	}
	out := PlaceOverlay(0, 0, "[box]", strings.Join(bgLines, "\n"), false, true)

	sgr := regexp.MustCompile("\x1b\\[[0-9;:]*m")
	for i, row := range strings.Split(out, "\n") {
		require.Equal(t, 38, ansi.PrintableRuneWidth(row), "row %d", i)
		if i == 2 {
			require.Contains(t, row, "[box]")
			continue
		}
		for _, seq := range sgr.FindAllString(row, -1) {
			require.Contains(t, []string{fadedText, fadedBackground, "\x1b[0m"}, seq, "row %d: %q", i, row)
		}
	}
}
