// Package proc reads process facts from /proc. keepon is Linux-only (it
// installs a systemd user unit), so /proc is always available.
package proc

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// root is the procfs mount point, overridden by tests with a fixture tree.
var root = "/proc"

// pageSize is how many bytes one page of resident memory accounts for.
var pageSize = int64(os.Getpagesize())

// Children returns pid's direct child PIDs, or nil if the process is gone.
func Children(pid int) []int {
	// The children file lives under the thread directory, not the process one.
	data, err := os.ReadFile(fmt.Sprintf("%s/%d/task/%d/children", root, pid, pid))
	if err != nil {
		return nil
	}
	var out []int
	for _, f := range strings.Fields(string(data)) {
		if child, err := strconv.Atoi(f); err == nil {
			out = append(out, child)
		}
	}
	return out
}

// HasChild reports whether pid has at least one child process. It is how keepon
// decides a pane is running its command rather than sitting at a bare shell
// prompt, which is more reliable than matching #{pane_current_command} against a
// list of shell names: that misjudges commands legitimately running inside a
// shell wrapper (a .sh script, "npm run dev") as never having started.
func HasChild(pid int) bool {
	return len(Children(pid)) > 0
}

// TreeRSS sums the resident set size of pid and all its descendants, in bytes.
// Pages shared between processes are counted once per process, so the total is
// an upper bound — good enough to answer "which session should I kill", which
// is what it is for.
func TreeRSS(pid int) int64 {
	// Guard against a cycle in a racing /proc read; a pane's tree is tiny, so a
	// visited set costs nothing.
	seen := map[int]bool{}
	var walk func(int) int64
	walk = func(p int) int64 {
		if seen[p] {
			return 0
		}
		seen[p] = true
		total := rss(p)
		for _, c := range Children(p) {
			total += walk(c)
		}
		return total
	}
	return walk(pid)
}

// rss returns one process's resident set size in bytes, or 0 if it has exited.
func rss(pid int) int64 {
	data, err := os.ReadFile(fmt.Sprintf("%s/%d/statm", root, pid))
	if err != nil {
		return 0
	}
	// statm is "size resident shared text lib data dt", all in pages.
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * pageSize
}

// FormatBytes renders n as a short human-readable size ("412 MB").
func FormatBytes(n int64) string {
	switch {
	case n <= 0:
		return "-"
	case n < 1<<20:
		return fmt.Sprintf("%d KB", n>>10)
	case n < 1<<30:
		return fmt.Sprintf("%d MB", n>>20)
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
}
