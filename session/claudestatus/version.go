package claudestatus

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var versionPattern = regexp.MustCompile(`\d+(\.\d+)+`)

// installedVersions caches what `claude --version` printed, by the binary's resolved path.
var installedVersions = struct {
	sync.Mutex
	byPath map[string]installedVersion
}{byPath: map[string]installedVersion{}}

type installedVersion struct {
	modTime time.Time
	size    int64
	version string
}

// InstalledVersion returns the version of Claude Code that program, a command line starting
// with the claude binary, runs when started now, or "" if it cannot tell. Claude Code updates
// itself on disk; running sessions keep the version they started with. Safe to call from
// several goroutines.
func InstalledVersion(program string) string {
	fields := strings.Fields(program)
	if len(fields) == 0 {
		return ""
	}
	path, err := exec.LookPath(fields[0])
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	// The native installer links the binary to ~/.local/share/claude/versions/<version>.
	if name := filepath.Base(resolved); filepath.Base(filepath.Dir(resolved)) == "versions" &&
		versionPattern.FindString(name) == name {
		return name
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return ""
	}
	installedVersions.Lock()
	defer installedVersions.Unlock()
	if v, ok := installedVersions.byPath[resolved]; ok && v.modTime.Equal(info.ModTime()) && v.size == info.Size() {
		return v.version
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	version := ""
	if err == nil {
		version = versionPattern.FindString(string(out))
	}
	// A failure is kept too, so it is not retried until the binary changes.
	installedVersions.byPath[resolved] = installedVersion{modTime: info.ModTime(), size: info.Size(), version: version}
	return version
}

// OlderVersion reports whether Claude Code version a, such as "2.1.296", is older than b. It
// is false if either is not a version.
func OlderVersion(a, b string) bool {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	for k := 0; k < max(len(va), len(vb)); k++ {
		var x, y int
		if k < len(va) {
			x = va[k]
		}
		if k < len(vb) {
			y = vb[k]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func parseVersion(v string) ([]int, bool) {
	v = versionPattern.FindString(v)
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	nums := make([]int, len(parts))
	for k, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		nums[k] = n
	}
	return nums, true
}
