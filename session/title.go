package session

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// UniqueTitle turns name into a title that is not in taken: at most 32 characters, with a
// number added if needed.
func UniqueTitle(name string, taken func(title string) bool) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		name = "session"
	}
	title := strings.TrimSpace(runewidth.Truncate(name, 32, ""))
	for n := 2; taken(title); n++ {
		suffix := fmt.Sprintf(" %d", n)
		title = strings.TrimSpace(runewidth.Truncate(name, 32-len(suffix), "")) + suffix
	}
	return title
}
