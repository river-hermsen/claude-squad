package session

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// sgrSequence matches an SGR escape sequence, which sets text attributes; otherSequence any
// other CSI or OSC escape sequence.
var (
	sgrSequence   = regexp.MustCompile(`^\x1b\[([0-9;]*)m`)
	otherSequence = regexp.MustCompile(`^(\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\))`)
)

// claudePromptIsEmpty reports whether pane content, captured with its escape sequences,
// shows Claude Code waiting for input with nothing typed: its prompt line, starting with
// "❯" between two horizontal rules, holds only blanks and dimmed placeholder text.
func claudePromptIsEmpty(content string) bool {
	lines := strings.Split(content, "\n")
	for k := len(lines) - 2; k >= 1; k-- {
		typed, isPrompt := promptText(lines[k])
		if !isPrompt {
			continue
		}
		above, _ := visibleText(lines[k-1])
		below, _ := visibleText(lines[k+1])
		if !strings.HasPrefix(above, "─") || !strings.HasPrefix(below, "─") {
			return false
		}
		return strings.TrimSpace(typed) == ""
	}
	return false
}

// promptText returns the text typed after the "❯" at the start of line, leaving out dimmed
// text, and whether line starts with "❯" at all.
func promptText(line string) (typed string, isPrompt bool) {
	text, styled := visibleText(line)
	if !strings.HasPrefix(text, "❯") {
		return "", false
	}
	var b strings.Builder
	for k, r := range []rune(text) {
		if k > 0 && !styled[k] {
			b.WriteRune(r)
		}
	}
	return b.String(), true
}

// visibleText strips escape sequences from line, and reports for each remaining rune
// whether it is dimmed.
func visibleText(line string) (string, []bool) {
	var text strings.Builder
	var dimmed []bool
	dim := false
	for len(line) > 0 {
		if m := sgrSequence.FindStringSubmatch(line); m != nil {
			params := strings.Split(m[1], ";")
			for p := 0; p < len(params); p++ {
				switch params[p] {
				case "", "0", "22":
					dim = false
				case "2":
					dim = true
				case "38", "48", "58":
					// An extended color: its arguments are not attributes.
					if p+1 < len(params) && params[p+1] == "5" {
						p += 2
					} else if p+1 < len(params) && params[p+1] == "2" {
						p += 4
					}
				}
			}
			line = line[len(m[0]):]
			continue
		}
		if m := otherSequence.FindString(line); m != "" {
			line = line[len(m):]
			continue
		}
		r, size := utf8.DecodeRuneInString(line)
		text.WriteRune(r)
		dimmed = append(dimmed, dim)
		line = line[size:]
	}
	return text.String(), dimmed
}
