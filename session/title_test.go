package session

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUniqueTitle(t *testing.T) {
	taken := map[string]bool{"fix the map": true, "fix the map 2": true}
	isTaken := func(title string) bool { return taken[title] }

	require.Equal(t, "fix the map 3", UniqueTitle("fix  the\tmap", isTaken))
	require.Equal(t, "session", UniqueTitle("  ", isTaken))
	long := UniqueTitle(strings.Repeat("abcdefghij", 5), isTaken)
	require.Equal(t, strings.Repeat("abcdefghij", 3)+"ab", long, "at most 32 characters")
}
