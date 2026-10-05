package session

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudePromptIsEmpty(t *testing.T) {
	const rule = "\x1b[38;5;244m────────────\x1b[39m"
	box := func(line string) string {
		return "⏺ done\n" + rule + "\n" + line + "\n" + rule + "\n  Opus 4.7 · status\n"
	}

	for _, tc := range []struct {
		name  string
		pane  string
		empty bool
	}{
		{"nothing typed", box("\x1b[39m❯ "), true},
		{"placeholder, as Claude Code 2.1 draws it", box("\x1b[39m❯ \x1b[2mTry \"create a util logging.py that...\"\x1b[0m"), true},
		{"cursor on an empty line", box("❯ \x1b[7m \x1b[27m"), true},
		{"text typed", box("❯ fix the \x1b[7mb\x1b[27mug"), false},
		{"text in a 256-color green, not dimmed", box("❯ \x1b[38;5;2mhello\x1b[39m"), false},
		{"permission dialog", "Do you want to make this edit?\n ❯ 1. Yes\n   2. No\n", false},
		{"resume picker", "  Resume session\n  ❯ Pong\n    11 seconds ago\n", false},
		{"no prompt at all", "loading…\n", false},
	} {
		require.Equal(t, tc.empty, claudePromptIsEmpty(tc.pane), tc.name)
	}
}
