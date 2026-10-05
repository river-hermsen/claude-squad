package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// This file turns `git diff` output into the diff tab's view: a header per file, line
// numbers, and tinted rows for added and removed lines, wrapped to the pane width.

var (
	diffAddedFg   = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	diffRemovedFg = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}

	diffAddedLineStyle   = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#e6ffec", Dark: "#1f3a2b"})
	diffRemovedLineStyle = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#ffebe9", Dark: "#45242a"})
	diffAddedMarkStyle   = diffAddedLineStyle.Foreground(diffAddedFg)
	diffRemovedMarkStyle = diffRemovedLineStyle.Foreground(diffRemovedFg)
	diffGutterStyle      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#8c959f", Dark: "#6e7681"})
	diffHunkStyle        = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"})
	diffNoteStyle        = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.AdaptiveColor{Light: "#8c959f", Dark: "#6e7681"})
	diffFileHeaderStyle  = lipgloss.NewStyle().Bold(true).
				Background(lipgloss.AdaptiveColor{Light: "#eaeef2", Dark: "#363b46"}).
				Foreground(lipgloss.AdaptiveColor{Light: "#1f2328", Dark: "#e6edf3"})
	diffFileStatusStyle = diffFileHeaderStyle.Bold(false).
				Foreground(lipgloss.AdaptiveColor{Light: "#656d76", Dark: "#9198a1"})
	diffFileAddedStyle   = diffFileHeaderStyle.Foreground(diffAddedFg)
	diffFileRemovedStyle = diffFileHeaderStyle.Foreground(diffRemovedFg)
)

// diffFile is one file of a parsed diff.
type diffFile struct {
	oldPath, newPath string
	// status is "new", "deleted" or "renamed", or "" for a modified file.
	status  string
	binary  bool
	added   int
	removed int
	hunks   []diffHunk
}

type diffHunk struct {
	// context is the text git puts after the @@ range, usually the enclosing function.
	context  string
	oldStart int
	newStart int
	lines    []diffLine
}

type diffLine struct {
	// kind is '+', '-', ' ' for context, or '\\' for "\ No newline at end of file".
	kind   byte
	text   string
	oldNum int
	newNum int
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// parseDiff parses the output of `git diff`.
func parseDiff(content string) []diffFile {
	var files []diffFile
	var file *diffFile
	var hunk *diffHunk
	oldNum, newNum := 0, 0
	// oldLeft and newLeft count the lines the current hunk still has, from its header.
	oldLeft, newLeft := 0, 0

	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			files = append(files, diffFile{})
			file = &files[len(files)-1]
			hunk = nil
			// Fallback paths, replaced by the ---/+++ lines when there are any.
			if a, b, ok := strings.Cut(strings.TrimPrefix(line, "diff --git "), " b/"); ok {
				file.oldPath, file.newPath = strings.TrimPrefix(a, "a/"), b
			}
			continue
		}
		if file == nil {
			continue
		}

		if hunk != nil && strings.HasPrefix(line, "\\") {
			// "\ No newline at end of file" follows the line it is about, even the last one.
			hunk.lines = append(hunk.lines, diffLine{kind: '\\', text: strings.TrimSpace(line[1:])})
			continue
		}
		if hunk != nil && (oldLeft > 0 || newLeft > 0) {
			kind := byte(' ')
			if len(line) > 0 {
				kind = line[0]
			}
			// A context line is " text"; tools that strip trailing spaces turn an empty one into "".
			text := ""
			if len(line) > 0 {
				text = line[1:]
			}
			switch kind {
			case '+':
				hunk.lines = append(hunk.lines, diffLine{kind: '+', text: text, newNum: newNum})
				newNum++
				newLeft--
				file.added++
				continue
			case '-':
				hunk.lines = append(hunk.lines, diffLine{kind: '-', text: text, oldNum: oldNum})
				oldNum++
				oldLeft--
				file.removed++
				continue
			case ' ':
				hunk.lines = append(hunk.lines, diffLine{kind: ' ', text: text, oldNum: oldNum, newNum: newNum})
				oldNum++
				newNum++
				oldLeft--
				newLeft--
				continue
			}
		}

		switch {
		case strings.HasPrefix(line, "@@"):
			m := hunkHeaderRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			oldNum, _ = strconv.Atoi(m[1])
			newNum, _ = strconv.Atoi(m[3])
			oldLeft, newLeft = hunkCount(m[2]), hunkCount(m[4])
			file.hunks = append(file.hunks, diffHunk{oldStart: oldNum, newStart: newNum, context: m[5]})
			hunk = &file.hunks[len(file.hunks)-1]
		case strings.HasPrefix(line, "new file mode"):
			file.status = "new"
		case strings.HasPrefix(line, "deleted file mode"):
			file.status = "deleted"
		case strings.HasPrefix(line, "rename from "):
			file.status = "renamed"
			file.oldPath = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			file.newPath = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "Binary files "):
			file.binary = true
		case strings.HasPrefix(line, "--- "):
			if p := strings.TrimPrefix(line, "--- "); p != "/dev/null" {
				file.oldPath = strings.TrimPrefix(p, "a/")
			}
		case strings.HasPrefix(line, "+++ "):
			if p := strings.TrimPrefix(line, "+++ "); p != "/dev/null" {
				file.newPath = strings.TrimPrefix(p, "b/")
			}
		}
	}
	return files
}

// hunkCount parses a line count from a hunk header, which git omits when it is 1.
func hunkCount(s string) int {
	if s == "" {
		return 1
	}
	n, _ := strconv.Atoi(s)
	return n
}

// renderDiff renders `git diff` output to fit width columns.
func renderDiff(content string, width int) string {
	if width < 20 {
		width = 20
	}
	var rows []string
	for i, f := range parseDiff(content) {
		if i > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, renderFileHeader(f, width))
		if f.binary {
			rows = append(rows, diffNoteStyle.Render("  Binary file changed"))
			continue
		}
		numWidth := lineNumberWidth(f)
		for j, h := range f.hunks {
			if j > 0 || h.oldStart > 1 || h.newStart > 1 {
				rows = append(rows, renderHunkHeader(h, numWidth, width))
			}
			for _, l := range h.lines {
				rows = append(rows, renderDiffLine(l, numWidth, width)...)
			}
		}
	}
	return strings.Join(rows, "\n")
}

func renderFileHeader(f diffFile, width int) string {
	path := f.newPath
	status := ""
	switch f.status {
	case "new":
		status = "new file"
	case "deleted":
		path, status = f.oldPath, "deleted"
	case "renamed":
		path = f.oldPath + " → " + f.newPath
		status = "renamed"
	}

	counts := diffFileAddedStyle.Render(fmt.Sprintf("+%d", f.added)) +
		diffFileHeaderStyle.Render(" ") +
		diffFileRemovedStyle.Render(fmt.Sprintf("-%d", f.removed)) +
		diffFileHeaderStyle.Render(" ")
	if f.binary {
		counts = diffFileStatusStyle.Render("binary ")
	}
	if status != "" {
		status = "  " + status
	}

	// Leave room for the counts; shorten the path from the left, keeping the file name.
	room := width - lipgloss.Width(counts) - runewidth.StringWidth(status) - 3
	if runewidth.StringWidth(path) > room && room > 1 {
		path = "…" + truncateLeft(path, room-1)
	}
	left := diffFileHeaderStyle.Render(" "+path) + diffFileStatusStyle.Render(status)
	gap := width - lipgloss.Width(left) - lipgloss.Width(counts)
	if gap < 1 {
		gap = 1
	}
	return left + diffFileHeaderStyle.Render(strings.Repeat(" ", gap)) + counts
}

func renderHunkHeader(h diffHunk, numWidth int, width int) string {
	label := fmt.Sprintf("@@ -%d +%d @@", h.oldStart, h.newStart)
	if h.context != "" {
		label += " " + h.context
	}
	gutter := strings.Repeat(" ", numWidth*2+1) + " ⋯ "
	return diffGutterStyle.Render(gutter) + diffHunkStyle.Render(truncate(label, width-runewidth.StringWidth(gutter)))
}

// renderDiffLine renders one diff line as one or more rows: the line numbers, the marker,
// then the text, wrapped to width. Added and removed rows are tinted across the full width.
func renderDiffLine(l diffLine, numWidth int, width int) []string {
	if l.kind == '\\' {
		return []string{diffGutterStyle.Render(strings.Repeat(" ", numWidth*2+2)) + diffNoteStyle.Render(l.text)}
	}

	gutterWidth := numWidth*2 + 2
	textWidth := width - gutterWidth - 2
	if textWidth < 1 {
		textWidth = 1
	}

	lineStyle, markStyle := lipgloss.NewStyle(), diffGutterStyle
	marker := " "
	switch l.kind {
	case '+':
		lineStyle, markStyle, marker = diffAddedLineStyle, diffAddedMarkStyle, "+"
	case '-':
		lineStyle, markStyle, marker = diffRemovedLineStyle, diffRemovedMarkStyle, "-"
	}

	chunks := wrapText(strings.ReplaceAll(l.text, "\t", "    "), textWidth)
	rows := make([]string, len(chunks))
	for i, chunk := range chunks {
		gutter := strings.Repeat(" ", gutterWidth)
		mark := " "
		if i == 0 {
			gutter = fmt.Sprintf("%*s %*s ", numWidth, lineNumber(l.oldNum), numWidth, lineNumber(l.newNum))
			mark = marker
		}
		padding := strings.Repeat(" ", textWidth-runewidth.StringWidth(chunk))
		rows[i] = diffGutterStyle.Render(gutter) + markStyle.Render(mark+" ") + lineStyle.Render(chunk+padding)
	}
	return rows
}

func lineNumber(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// lineNumberWidth returns the number of digits of the largest line number in the file.
func lineNumberWidth(f diffFile) int {
	largest := 0
	for _, h := range f.hunks {
		for _, l := range h.lines {
			largest = max(largest, max(l.oldNum, l.newNum))
		}
	}
	return max(len(strconv.Itoa(largest)), 2)
}

// wrapText splits s into pieces at most width columns wide, breaking after a space when
// there is one late enough in the piece to keep words whole. It always returns at least one
// piece, so empty lines still get a row.
func wrapText(s string, width int) []string {
	var chunks []string
	var line []rune
	w := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		for w+rw > width && len(line) > 0 {
			cut := len(line)
			for i := len(line) - 1; i > 0 && runewidth.StringWidth(string(line[:i])) > width/3; i-- {
				if line[i] == ' ' {
					cut = i + 1
					break
				}
			}
			chunks = append(chunks, string(line[:cut]))
			line = append([]rune(nil), line[cut:]...)
			w = runewidth.StringWidth(string(line))
		}
		line = append(line, r)
		w += rw
	}
	return append(chunks, string(line))
}

func truncate(s string, width int) string {
	if width < 1 {
		return ""
	}
	return runewidth.Truncate(s, width, "…")
}

// truncateLeft returns the last width columns of s.
func truncateLeft(s string, width int) string {
	runes := []rune(s)
	w := 0
	i := len(runes)
	for i > 0 && w+runewidth.RuneWidth(runes[i-1]) <= width {
		i--
		w += runewidth.RuneWidth(runes[i])
	}
	return string(runes[i:])
}
