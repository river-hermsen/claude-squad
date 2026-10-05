package keys

import (
	"github.com/charmbracelet/bubbles/key"
)

type KeyName int

const (
	KeyUp KeyName = iota
	KeyDown
	KeyEnter
	KeyNew
	KeyKill
	KeyQuit
	KeyReview
	KeyPush

	KeyTab        // Tab is a special keybinding for switching between panes.
	KeySubmitName // SubmitName is a special keybinding for submitting the name of a new instance.

	KeyCheckout
	KeyResume
	KeyPrompt // New key for entering a prompt
	KeyHelp   // Key for showing help screen

	// Diff keybindings
	KeyShiftUp
	KeyShiftDown

	// Reorder keybindings
	KeyMoveUp
	KeyMoveDown

	// Repository selection in the diff tab of multi-repo instances
	KeyPrevRepo
	KeyNextRepo

	KeyRename
	KeyFork
	// KeyResumeClaude starts an instance that resumes a Claude Code conversation.
	KeyResumeClaude
	// KeyFocus sends the keys that follow to the selected session, without attaching to it.
	KeyFocus
	// KeyFocusExit is KeyFocus pressed again, while typing into a session; see KeyFocus.
	KeyFocusExit
	// KeyRemote shows the selected session's Remote Control link, or turns Remote Control on.
	KeyRemote
)

// GlobalKeyStringsMap is a global, immutable map string to keybinding.
var GlobalKeyStringsMap = map[string]KeyName{
	"up":         KeyUp,
	"k":          KeyUp,
	"down":       KeyDown,
	"j":          KeyDown,
	"shift+up":   KeyShiftUp,
	"shift+down": KeyShiftDown,
	"J":          KeyMoveDown,
	"K":          KeyMoveUp,
	"N":          KeyPrompt,
	"enter":      KeyEnter,
	"o":          KeyEnter,
	"n":          KeyNew,
	"D":          KeyKill,
	"q":          KeyQuit,
	"tab":        KeyTab,
	"c":          KeyCheckout,
	"r":          KeyResume,
	"?":          KeyHelp,
	"left":       KeyPrevRepo,
	"right":      KeyNextRepo,
	"R":          KeyRename,
	"f":          KeyFork,
	"C":          KeyResumeClaude,
	"`":          KeyFocus,
	"w":          KeyRemote,
}

// GlobalkeyBindings is a global, immutable map of KeyName tot keybinding.
var GlobalkeyBindings = map[KeyName]key.Binding{
	KeyUp: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	KeyDown: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	KeyShiftUp: key.NewBinding(
		key.WithKeys("shift+up"),
		key.WithHelp("shift+↑", "scroll"),
	),
	KeyShiftDown: key.NewBinding(
		key.WithKeys("shift+down"),
		key.WithHelp("shift+↓", "scroll"),
	),
	KeyEnter: key.NewBinding(
		key.WithKeys("enter", "o"),
		key.WithHelp("↵/o", "open"),
	),
	KeyNew: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "new"),
	),
	KeyKill: key.NewBinding(
		key.WithKeys("D"),
		key.WithHelp("D", "kill"),
	),
	KeyHelp: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	KeyQuit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	KeyPrompt: key.NewBinding(
		key.WithKeys("N"),
		key.WithHelp("N", "new with prompt"),
	),
	KeyCheckout: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "checkout"),
	),
	KeyTab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch tab"),
	),
	KeyResume: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "resume"),
	),

	KeyMoveUp: key.NewBinding(
		key.WithKeys("K"),
		key.WithHelp("K", "move up"),
	),
	KeyMoveDown: key.NewBinding(
		key.WithKeys("J"),
		key.WithHelp("J", "move down"),
	),

	KeyPrevRepo: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←", "previous repo"),
	),
	KeyNextRepo: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("←/→", "switch repo"),
	),

	KeyRename: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "rename"),
	),
	KeyFork: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "fork"),
	),
	KeyResumeClaude: key.NewBinding(
		key.WithKeys("C"),
		key.WithHelp("C", "resume claude"),
	),
	KeyFocus: key.NewBinding(
		key.WithKeys("`"),
		key.WithHelp("`", "type"),
	),
	KeyRemote: key.NewBinding(
		key.WithKeys("w"),
		key.WithHelp("w", "remote"),
	),

	// -- Special keybindings --

	KeyFocusExit: key.NewBinding(
		key.WithKeys("`"),
		key.WithHelp("`", "back to list"),
	),

	KeySubmitName: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit name"),
	),
}
