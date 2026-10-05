package ui

import (
	"errors"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type ErrBox struct {
	height, width int
	err           error
	// info is true if err is a notice rather than an error; see SetInfo.
	info bool
}

var errStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#FF0000",
	Dark:  "#FF0000",
})

var infoStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#1a6fa8",
	Dark:  "#4aa3df",
})

func NewErrBox() *ErrBox {
	return &ErrBox{}
}

func (e *ErrBox) SetError(err error) {
	e.err = err
	e.info = false
}

// SetInfo shows msg, a notice such as that a link was copied, in place of an error.
func (e *ErrBox) SetInfo(msg string) {
	e.err = errors.New(msg)
	e.info = true
}

func (e *ErrBox) Clear() {
	e.err = nil
}

func (e *ErrBox) SetSize(width, height int) {
	e.width = width
	e.height = height
}

func (e *ErrBox) String() string {
	var err string
	if e.err != nil {
		err = e.err.Error()
		lines := strings.Split(err, "\n")
		err = strings.Join(lines, "//")
		if runewidth.StringWidth(err) > e.width-3 && e.width-3 >= 0 {
			err = runewidth.Truncate(err, e.width-3, "...")
		}
	}
	style := errStyle
	if e.info {
		style = infoStyle
	}
	return lipgloss.Place(e.width, e.height, lipgloss.Center, lipgloss.Center, style.Render(err))
}
