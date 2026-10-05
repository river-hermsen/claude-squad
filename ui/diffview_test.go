package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"
)

const sampleDiff = `diff --git a/src/app.cs b/src/app.cs
index 1111111..2222222 100644
--- a/src/app.cs
+++ b/src/app.cs
@@ -1,3 +1,4 @@
 using System;
+using System.Linq;

 class App {
@@ -40,2 +41,2 @@ class App {
-	void Run() {}
+	void Run(int times) {}
 }
diff --git a/docs/new.md b/docs/new.md
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/docs/new.md
@@ -0,0 +1 @@
+hello
\ No newline at end of file
diff --git a/old.txt b/old.txt
deleted file mode 100644
index 4444444..0000000
--- a/old.txt
+++ /dev/null
@@ -1 +0,0 @@
-bye
diff --git a/a.txt b/b.txt
similarity index 90%
rename from a.txt
rename to b.txt
diff --git a/logo.png b/logo.png
index 5555555..6666666 100644
Binary files a/logo.png and b/logo.png differ
`

func TestParseDiff(t *testing.T) {
	files := parseDiff(sampleDiff)
	require.Len(t, files, 5)

	app := files[0]
	require.Equal(t, "src/app.cs", app.newPath)
	require.Equal(t, 2, app.added)
	require.Equal(t, 1, app.removed)
	require.Len(t, app.hunks, 2)
	require.Equal(t, diffLine{kind: '+', text: "using System.Linq;", newNum: 2}, app.hunks[0].lines[1])
	require.Equal(t, diffLine{kind: ' ', text: "class App {", oldNum: 3, newNum: 4}, app.hunks[0].lines[3])
	require.Equal(t, "class App {", app.hunks[1].context)
	require.Equal(t, diffLine{kind: '-', text: "\tvoid Run() {}", oldNum: 40}, app.hunks[1].lines[0])

	require.Equal(t, "new", files[1].status)
	require.Equal(t, "docs/new.md", files[1].newPath)
	require.Equal(t, byte('\\'), files[1].hunks[0].lines[1].kind)

	require.Equal(t, "deleted", files[2].status)
	require.Equal(t, "old.txt", files[2].oldPath)

	require.Equal(t, "renamed", files[3].status)
	require.Equal(t, "a.txt", files[3].oldPath)
	require.Equal(t, "b.txt", files[3].newPath)

	require.True(t, files[4].binary)
}

func TestRenderDiff(t *testing.T) {
	out := renderDiff(sampleDiff, 80)

	for _, raw := range []string{"diff --git", "index 1111111", "--- a/src/app.cs", "+++ b/src/app.cs"} {
		require.NotContains(t, out, raw, "git's header lines are replaced by the file header")
	}
	require.Contains(t, out, " src/app.cs")
	require.Contains(t, out, "+2 -1")
	require.Contains(t, out, "docs/new.md  new file")
	require.Contains(t, out, "old.txt  deleted")
	require.Contains(t, out, "a.txt → b.txt  renamed")
	require.Contains(t, out, "Binary file changed")
	require.Contains(t, out, "No newline at end of file")

	lines := strings.Split(out, "\n")
	// Rows are padded to the full width, so added and removed lines are tinted edge to edge.
	padded := func(s string) string { return s + strings.Repeat(" ", 80-len(s)) }
	require.Contains(t, lines, padded("    2 + using System.Linq;"))
	require.Contains(t, lines, padded(" 3  4   class App {"))
	require.Contains(t, out, "@@ -40 +41 @@ class App {", "later hunks get a separator")
	require.NotContains(t, out, "@@ -1 +1 @@", "a hunk at the top of the file needs none")
	require.Contains(t, out, "40    - "+"    void Run() {}", "tabs are expanded")

	for _, line := range lines {
		require.LessOrEqual(t, lipgloss.Width(line), 80, "row wider than the pane: %q", line)
	}
}

func TestRenderDiffWrapsLongLines(t *testing.T) {
	long := strings.Repeat("x", 100)
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-short\n+" + long + "\n"

	out := renderDiff(diff, 40)
	var rows []string
	for _, line := range strings.Split(out, "\n") {
		require.LessOrEqual(t, lipgloss.Width(line), 40)
		if strings.Contains(line, "x") {
			rows = append(rows, line)
		}
	}
	require.Len(t, rows, 4, "100 columns wrap into rows of 32: 40 minus 6 for line numbers and 2 for the marker")
	require.Equal(t, 100, strings.Count(strings.Join(rows, ""), "x"), "nothing is cut off")
	require.True(t, strings.HasPrefix(rows[1], "      "), "continuation rows have an empty gutter")
}

func TestRenderFileHeaderShortensLongPaths(t *testing.T) {
	f := diffFile{newPath: "src/very/deeply/nested/directory/structure/Component.vue", added: 3}
	header := renderFileHeader(f, 40)
	require.Equal(t, 40, lipgloss.Width(header))
	require.Contains(t, header, "…")
	require.Contains(t, header, "Component.vue", "the file name survives")
	require.Contains(t, header, "+3 -0")
}

func TestWrapTextKeepsWordsWhole(t *testing.T) {
	require.Equal(t, []string{"lines.Sum(l => ", "l.Quantity * ", "l.UnitPrice)"}, wrapText("lines.Sum(l => l.Quantity * l.UnitPrice)", 15))
	require.Equal(t, []string{"abcdefghij", "klm"}, wrapText("abcdefghijklm", 10), "no space: hard break")
	require.Equal(t, []string{"a bcdefghij", "klm"}, wrapText("a bcdefghijklm", 11), "a space too early is not used")
	require.Equal(t, []string{""}, wrapText("", 10))
}
