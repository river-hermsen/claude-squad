package overlay

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const (
	// maxFolderCompletions bounds how many subdirectories of one directory the picker lists.
	maxFolderCompletions = 50
	// folderRows is how many folders the picker shows at once.
	folderRows = 8
)

var folderErrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))

// FolderPicker asks for the folder a new session starts in. What is typed is a path, relative
// to baseDir unless it starts with / or ~. Below it are the folders to choose from: with nothing
// typed the recent ones, otherwise the typed folder itself, the recent folders that contain the
// text, and the subdirectories it completes to. ↑/↓ move between them, Tab completes the input
// to the highlighted one, Enter chooses it and Esc cancels.
type FolderPicker struct {
	input   textinput.Model
	title   string
	width   int
	baseDir string
	home    string
	recent  []string

	candidates []string
	cursor     int

	submitted bool
	chosen    string
	err       string
}

// NewFolderPicker returns a picker titled title that resolves relative paths against baseDir
// and offers recent, absolute paths of folders, first, while nothing is typed.
func NewFolderPicker(title string, baseDir string, recent []string) *FolderPicker {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "type a path, or pick a folder below"
	input.Focus()
	home, _ := os.UserHomeDir()
	p := &FolderPicker{input: input, title: title, width: 70, baseDir: baseDir, home: home, recent: recent}
	p.refresh()
	return p
}

// SetWidth sets the overlay's width.
func (p *FolderPicker) SetWidth(width int) {
	p.width = width
}

// HandleKeyPress handles a key and reports whether the picker is done: a folder chosen with
// Enter, or canceled with Esc.
func (p *FolderPicker) HandleKeyPress(msg tea.KeyMsg) (done bool) {
	switch msg.Type {
	case tea.KeyEsc:
		return true
	case tea.KeyEnter:
		dir := p.selected()
		if dir == "" {
			p.err = "type a folder or pick one"
			return false
		}
		if !isDir(dir) {
			p.err = "no such folder: " + p.display(dir)
			return false
		}
		p.chosen, p.submitted = dir, true
		return true
	case tea.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return false
	case tea.KeyDown:
		if p.cursor < len(p.candidates)-1 {
			p.cursor++
		}
		return false
	case tea.KeyTab:
		if dir := p.selected(); dir != "" {
			p.input.SetValue(strings.TrimSuffix(p.display(dir), "/") + "/")
			p.input.CursorEnd()
			p.refresh()
		}
		return false
	}
	before := p.input.Value()
	p.input, _ = p.input.Update(msg)
	if p.input.Value() != before {
		p.refresh()
	}
	return false
}

// Submitted reports whether a folder was chosen.
func (p *FolderPicker) Submitted() bool {
	return p.submitted
}

// Value returns the absolute path of the chosen folder.
func (p *FolderPicker) Value() string {
	return p.chosen
}

// selected returns the highlighted folder, or else the typed one, or "" if nothing is typed.
func (p *FolderPicker) selected() string {
	if len(p.candidates) > 0 {
		return p.candidates[p.cursor]
	}
	if query := strings.TrimSpace(p.input.Value()); query != "" {
		return p.resolve(query)
	}
	return ""
}

// refresh lists the folders that match what is typed, and highlights the first.
func (p *FolderPicker) refresh() {
	p.err = ""
	p.cursor = 0
	var out []string
	seen := map[string]bool{}
	add := func(dir string) {
		if !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}

	query := strings.TrimSpace(p.input.Value())
	if query == "" {
		for _, dir := range p.recent {
			add(dir)
		}
		p.candidates = out
		return
	}

	path := p.resolve(query)
	if isDir(path) {
		add(path)
	}
	lower := strings.ToLower(query)
	for _, dir := range p.recent {
		if strings.Contains(strings.ToLower(p.display(dir)), lower) {
			add(dir)
		}
	}
	// "a/b" completes to a's subdirectories starting with b, "a/b/" lists all of b's.
	dir, prefix := path, ""
	if !strings.HasSuffix(query, "/") {
		dir, prefix = filepath.Dir(path), filepath.Base(path)
	}
	for _, name := range subdirs(dir, prefix) {
		add(filepath.Join(dir, name))
	}
	p.candidates = out
}

// resolve turns query, a typed path, into an absolute one.
func (p *FolderPicker) resolve(query string) string {
	switch {
	case query == "~":
		return p.home
	case strings.HasPrefix(query, "~/"):
		return filepath.Join(p.home, query[2:])
	case filepath.IsAbs(query):
		return filepath.Clean(query)
	}
	return filepath.Join(p.baseDir, query)
}

// display shortens the home directory at the start of dir to ~.
func (p *FolderPicker) display(dir string) string {
	if p.home != "" && (dir == p.home || strings.HasPrefix(dir, p.home+string(filepath.Separator))) {
		return "~" + dir[len(p.home):]
	}
	return dir
}

// subdirs returns the names of dir's subdirectories that start with prefix, ignoring case;
// hidden ones only if prefix starts with a dot.
func subdirs(dir, prefix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	lower := strings.ToLower(prefix)
	var names []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), lower) {
			continue
		}
		if !e.IsDir() && !(e.Type()&os.ModeSymlink != 0 && isDir(filepath.Join(dir, name))) {
			continue
		}
		names = append(names, name)
		if len(names) == maxFolderCompletions {
			break
		}
	}
	return names
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Render renders the overlay.
func (p *FolderPicker) Render() string {
	inner := p.width - tiStyle.GetHorizontalFrameSize()
	p.input.Width = max(inner-1, 1)

	var s strings.Builder
	s.WriteString(tiTitleStyle.Render(p.title))
	s.WriteString("\n")
	s.WriteString(p.input.View())
	s.WriteString("\n\n")

	if len(p.candidates) == 0 {
		s.WriteString(bpDimStyle.Render("  No matching folders"))
	}
	start := 0
	if p.cursor >= folderRows {
		start = p.cursor - folderRows + 1
	}
	end := min(start+folderRows, len(p.candidates))
	for i := start; i < end; i++ {
		label := p.display(p.candidates[i])
		// A long path keeps its end, which names the folder.
		if room := inner - 2; runewidth.StringWidth(label) > room {
			label = "…" + truncateLeft(label, room-1)
		}
		if i == p.cursor {
			s.WriteString(bpSelectedStyle.Render("> " + label))
		} else {
			s.WriteString(bpDimStyle.Render("  " + label))
		}
		if i < end-1 {
			s.WriteString("\n")
		}
	}

	s.WriteString("\n\n")
	if p.err != "" {
		s.WriteString(folderErrStyle.Render(p.err))
	} else {
		s.WriteString(inputHintStyle.Render("↑↓ pick · tab complete · enter choose · esc cancel"))
	}
	return tiStyle.Width(p.width - tiStyle.GetHorizontalFrameSize() + tiStyle.GetHorizontalPadding()).Render(s.String())
}

// truncateLeft returns the end of s that is at most width columns wide.
func truncateLeft(s string, width int) string {
	runes := []rune(s)
	used := 0
	i := len(runes)
	for i > 0 {
		w := runewidth.RuneWidth(runes[i-1])
		if used+w > width {
			break
		}
		used += w
		i--
	}
	return string(runes[i:])
}
