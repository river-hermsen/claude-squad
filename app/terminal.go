package app

import "os"

// selectionKey names the key that, held while dragging, makes the terminal cs runs in select
// text itself, for its own copy, while cs takes the mouse. Over SSH only TERM tells terminals
// apart, which macOS Terminal and iTerm2 do not set to anything of their own, so it lists
// the candidates when it cannot tell.
func selectionKey() string {
	switch {
	case os.Getenv("TERM") == "xterm-ghostty" || os.Getenv("TERM_PROGRAM") == "ghostty":
		return "Shift"
	case os.Getenv("LC_TERMINAL") == "iTerm2" || os.Getenv("TERM_PROGRAM") == "iTerm.app":
		return "Option"
	case os.Getenv("TERM_PROGRAM") == "Apple_Terminal":
		return "Fn"
	default:
		return "Fn (Terminal), Option (iTerm2) or Shift (Ghostty)"
	}
}
