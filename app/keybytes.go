package app

import tea "github.com/charmbracelet/bubbletea"

// keySequences are the escape sequences an xterm-style terminal sends for keys that are not
// a single character.
var keySequences = map[tea.KeyType]string{
	tea.KeyUp:         "\x1b[A",
	tea.KeyDown:       "\x1b[B",
	tea.KeyRight:      "\x1b[C",
	tea.KeyLeft:       "\x1b[D",
	tea.KeyShiftUp:    "\x1b[1;2A",
	tea.KeyShiftDown:  "\x1b[1;2B",
	tea.KeyShiftRight: "\x1b[1;2C",
	tea.KeyShiftLeft:  "\x1b[1;2D",
	tea.KeyCtrlUp:     "\x1b[1;5A",
	tea.KeyCtrlDown:   "\x1b[1;5B",
	tea.KeyCtrlRight:  "\x1b[1;5C",
	tea.KeyCtrlLeft:   "\x1b[1;5D",
	tea.KeyShiftTab:   "\x1b[Z",
	tea.KeyHome:       "\x1b[H",
	tea.KeyEnd:        "\x1b[F",
	tea.KeyPgUp:       "\x1b[5~",
	tea.KeyPgDown:     "\x1b[6~",
	tea.KeyInsert:     "\x1b[2~",
	tea.KeyDelete:     "\x1b[3~",
	tea.KeyF1:         "\x1bOP",
	tea.KeyF2:         "\x1bOQ",
	tea.KeyF3:         "\x1bOR",
	tea.KeyF4:         "\x1bOS",
}

// keyBytes returns what a terminal sends for msg, for typing into a session through its
// tmux client, or nil for a key it cannot send. Pastes keep their bracketed-paste markers,
// so multi-line text arrives as one paste instead of separate lines.
func keyBytes(msg tea.KeyMsg) []byte {
	if msg.Paste {
		return []byte("\x1b[200~" + string(msg.Runes) + "\x1b[201~")
	}
	var out []byte
	switch {
	case msg.Type == tea.KeyRunes:
		out = []byte(string(msg.Runes))
	case msg.Type == tea.KeySpace:
		out = []byte(" ")
	case keySequences[msg.Type] != "":
		out = []byte(keySequences[msg.Type])
	case msg.Type >= 0 && msg.Type <= 31, msg.Type == 127:
		// Control characters, among them enter, tab, escape, backspace and ctrl+letter.
		out = []byte{byte(msg.Type)}
	default:
		return nil
	}
	if msg.Alt {
		out = append([]byte{0x1b}, out...)
	}
	return out
}
