package overlay

import (
	"strconv"
	"strings"
)

const (
	fadedText       = "\x1b[0;38;5;240m"          // medium gray text, no other attributes
	fadedBackground = "\x1b[0;38;5;240;48;5;236m" // the same on a dark gray background
)

// fadeLine renders a line behind an overlay in one gray. Every SGR sequence becomes
// fadedText, or fadedBackground while the line had a background color, so no text keeps its
// own color, boldness or the terminal's default color. Other escape sequences pass through.
func fadeLine(line string) string {
	var b strings.Builder
	b.WriteString(fadedText)
	hasBackground := false
	for i := 0; i < len(line); {
		if line[i] != '\x1b' || i+1 >= len(line) || line[i+1] != '[' {
			b.WriteByte(line[i])
			i++
			continue
		}
		// A CSI sequence: parameter bytes, intermediate bytes, then one final byte.
		end := i + 2
		for end < len(line) && line[end] >= 0x20 && line[end] <= 0x3f {
			end++
		}
		if end >= len(line) {
			b.WriteString(line[i:])
			break
		}
		if line[end] != 'm' {
			b.WriteString(line[i : end+1])
			i = end + 1
			continue
		}
		hasBackground = sgrBackground(line[i+2:end], hasBackground)
		if hasBackground {
			b.WriteString(fadedBackground)
		} else {
			b.WriteString(fadedText)
		}
		i = end + 1
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// sgrBackground reports whether a background color is set after the SGR parameters params,
// given whether one was set before.
func sgrBackground(params string, hasBackground bool) bool {
	if params == "" {
		return false // ESC[m resets
	}
	codes := strings.Split(params, ";")
	for i := 0; i < len(codes); i++ {
		// Sub-parameters separated by colons, as in 38:2::255:0:0, belong to their code.
		code, err := strconv.Atoi(strings.SplitN(codes[i], ":", 2)[0])
		if err != nil && codes[i] != "" {
			continue
		}
		switch {
		case code == 0 || code == 49:
			hasBackground = false
		case code >= 40 && code <= 47, code >= 100 && code <= 107:
			hasBackground = true
		case code == 38 || code == 48 || code == 58:
			if code == 48 {
				hasBackground = true
			}
			// Skip the color's own arguments, so 38;5;0 is not read as a reset.
			if strings.Contains(codes[i], ":") || i+1 >= len(codes) {
				continue
			}
			switch codes[i+1] {
			case "5":
				i += 2
			case "2":
				i += 4
			}
		}
	}
	return hasBackground
}
