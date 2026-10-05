package overlay

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	qrcode "github.com/skip2/go-qrcode"
)

// RemoteChoice is what the user picked in a RemoteControlOverlay.
type RemoteChoice int

const (
	// RemoteClose closes the overlay.
	RemoteClose RemoteChoice = iota
	// RemoteOpen opens the link in a browser.
	RemoteOpen
	// RemoteDisconnect disconnects the session from claude.ai.
	RemoteDisconnect
)

var (
	remoteTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4aa3df"))
	remoteURLStyle   = lipgloss.NewStyle().Bold(true).Underline(true)
	remoteDimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	remoteKeyStyle   = lipgloss.NewStyle().Bold(true)
	// A QR code reads as dark modules on light ground whatever the terminal's colors are.
	remoteQRStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#000000"))
)

// RemoteControlOverlay shows where Remote Control continues a session: its link on
// claude.ai/code, and a QR code of that link for the Claude app on a phone.
type RemoteControlOverlay struct {
	title  string
	url    string
	note   string
	qr     []string
	choice RemoteChoice
}

// NewRemoteControlOverlay returns an overlay for the session titled title, connected at url.
// note says what happened to the link, such as that it was copied. The QR code is left out
// if the overlay would be taller than maxHeight rows.
func NewRemoteControlOverlay(title, url, note string, maxHeight int) *RemoteControlOverlay {
	o := &RemoteControlOverlay{title: title, url: url, note: note}
	if code, err := qrcode.New(url, qrcode.Low); err == nil {
		qr := strings.Split(strings.TrimRight(code.ToSmallString(false), "\n"), "\n")
		// The rest of the overlay takes about 14 rows.
		if len(qr)+14 <= maxHeight {
			o.qr = qr
		}
	}
	return o
}

// HandleKeyPress handles a key. Every key closes the overlay; o and d also pick an action.
func (o *RemoteControlOverlay) HandleKeyPress(msg tea.KeyMsg) (done bool) {
	switch msg.String() {
	case "o":
		o.choice = RemoteOpen
	case "d":
		o.choice = RemoteDisconnect
	default:
		o.choice = RemoteClose
	}
	return true
}

// Choice returns what the user picked when closing the overlay.
func (o *RemoteControlOverlay) Choice() RemoteChoice {
	return o.choice
}

// HasQRCode reports whether the overlay shows a QR code.
func (o *RemoteControlOverlay) HasQRCode() bool {
	return len(o.qr) > 0
}

// Render renders the overlay.
func (o *RemoteControlOverlay) Render(opts ...WhitespaceOption) string {
	lines := []string{
		remoteTitleStyle.Render("Remote Control") + remoteDimStyle.Render(" · "+o.title),
		"",
		"Continue this session at",
		remoteURLStyle.Render(o.url),
	}
	if o.note != "" {
		lines = append(lines, remoteDimStyle.Render(o.note))
	}
	if len(o.qr) > 0 {
		lines = append(lines, "", remoteDimStyle.Render("or scan it with your phone for the Claude app:"))
		for _, row := range o.qr {
			lines = append(lines, remoteQRStyle.Render(row))
		}
	}
	lines = append(lines,
		"",
		remoteDimStyle.Render("It keeps running here; claude.ai and the app only connect to it."),
		remoteKeyStyle.Render("o")+remoteDimStyle.Render(" open in browser · ")+
			remoteKeyStyle.Render("d")+remoteDimStyle.Render(" disconnect · any other key closes"),
	)

	width := 0
	for _, line := range lines {
		width = max(width, lipgloss.Width(line))
	}
	width = max(width, runewidth.StringWidth(o.url))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4aa3df")).
		Padding(1, 2).
		Width(width + 4).
		Render(strings.Join(lines, "\n"))
}
