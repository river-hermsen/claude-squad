package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// Over SSH there is no clipboard of the machine's own, so the link goes to the terminal's
// clipboard with OSC 52, which reaches the computer the SSH session comes from.
func TestCopyToClipboardFallsBackToTerminal(t *testing.T) {
	t.Setenv("TMUX", "")
	defer func(saved func(string) error) { writeClipboard = saved }(writeClipboard)

	writeClipboard = func(string) error { return errors.New("no clipboard utilities available") }
	var out bytes.Buffer
	note := copyToClipboard("https://claude.ai/code/session_01ABC", &out)
	require.Contains(t, note, "OSC 52")
	require.Equal(t, "\x1b]52;c;"+base64.StdEncoding.EncodeToString([]byte("https://claude.ai/code/session_01ABC"))+"\x07", out.String())

	var copied string
	writeClipboard = func(text string) error { copied = text; return nil }
	out.Reset()
	require.Equal(t, "Copied to the clipboard.", copyToClipboard("https://claude.ai/code/session_01ABC", &out))
	require.Equal(t, "https://claude.ai/code/session_01ABC", copied)
	require.Empty(t, out.String())
}
