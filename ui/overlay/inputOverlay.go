package overlay

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var inputHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// InputOverlay asks for one line of text, such as a session title. Enter submits it and
// Esc cancels.
type InputOverlay struct {
	input     textinput.Model
	title     string
	width     int
	submitted bool
}

// NewInputOverlay returns an overlay titled title, holding value, that takes at most
// charLimit characters.
func NewInputOverlay(title string, value string, charLimit int) *InputOverlay {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = charLimit
	input.SetValue(value)
	input.CursorEnd()
	input.Focus()
	return &InputOverlay{input: input, title: title, width: 50}
}

// SetWidth sets the overlay's width.
func (o *InputOverlay) SetWidth(width int) {
	o.width = width
}

// HandleKeyPress handles a key and reports whether the overlay is done: submitted with
// Enter or canceled with Esc.
func (o *InputOverlay) HandleKeyPress(msg tea.KeyMsg) (done bool) {
	switch msg.Type {
	case tea.KeyEnter:
		o.submitted = true
		return true
	case tea.KeyEsc:
		return true
	}
	o.input, _ = o.input.Update(msg)
	return false
}

// Submitted reports whether the overlay was closed with Enter.
func (o *InputOverlay) Submitted() bool {
	return o.submitted
}

// Value returns the text entered.
func (o *InputOverlay) Value() string {
	return o.input.Value()
}

// Render renders the overlay.
func (o *InputOverlay) Render() string {
	inner := o.width - tiStyle.GetHorizontalFrameSize()
	o.input.Width = max(inner-1, 1)
	content := tiTitleStyle.Render(o.title) + "\n" +
		o.input.View() + "\n\n" +
		inputHintStyle.Render("enter to confirm · esc to cancel")
	return tiStyle.Width(o.width - tiStyle.GetHorizontalFrameSize() + tiStyle.GetHorizontalPadding()).Render(content)
}
