package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fakeProc builds a /proc-shaped tree: tree maps a pid to its children, rss
// maps a pid to its resident page count.
func fakeProc(t *testing.T, tree map[int][]int, rssPages map[int]int64) {
	t.Helper()
	dir := t.TempDir()
	for pid, kids := range tree {
		taskDir := filepath.Join(dir, fmt.Sprint(pid), "task", fmt.Sprint(pid))
		if err := os.MkdirAll(taskDir, 0o755); err != nil {
			t.Fatal(err)
		}
		var children string
		for _, k := range kids {
			children += fmt.Sprintf("%d ", k)
		}
		if err := os.WriteFile(filepath.Join(taskDir, "children"), []byte(children+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		statm := fmt.Sprintf("1000 %d 0 0 0 0 0\n", rssPages[pid])
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprint(pid), "statm"), []byte(statm), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old, oldPage := root, pageSize
	root, pageSize = dir, 4096
	t.Cleanup(func() { root, pageSize = old, oldPage })
}

func TestChildren(t *testing.T) {
	fakeProc(t, map[int][]int{10: {11, 12}, 11: nil, 12: nil}, nil)

	if got := Children(10); len(got) != 2 || got[0] != 11 || got[1] != 12 {
		t.Errorf("Children(10) = %v, want [11 12]", got)
	}
	if got := Children(11); got != nil {
		t.Errorf("Children(11) = %v, want nil", got)
	}
	if got := Children(999); got != nil {
		t.Errorf("Children of missing pid = %v, want nil", got)
	}
}

// TestHasChild covers the distinction keepon actually cares about: a pane whose
// shell spawned the run command versus one sitting at a bare prompt.
func TestHasChild(t *testing.T) {
	fakeProc(t, map[int][]int{10: {11}, 11: nil, 20: nil}, nil)

	if !HasChild(10) {
		t.Error("HasChild(10) = false, want true (shell has the command running)")
	}
	if HasChild(20) {
		t.Error("HasChild(20) = true, want false (bare shell)")
	}
	if HasChild(999) {
		t.Error("HasChild of missing pid = true, want false")
	}
}

func TestTreeRSS(t *testing.T) {
	// 10 -> 11 -> 12, one page each at the root and 100/50 pages below.
	fakeProc(t,
		map[int][]int{10: {11}, 11: {12}, 12: nil},
		map[int]int64{10: 1, 11: 100, 12: 50},
	)

	want := int64(151 * 4096)
	if got := TreeRSS(10); got != want {
		t.Errorf("TreeRSS(10) = %d, want %d", got, want)
	}
	if got := TreeRSS(12); got != 50*4096 {
		t.Errorf("TreeRSS(12) = %d, want %d", got, 50*4096)
	}
	if got := TreeRSS(999); got != 0 {
		t.Errorf("TreeRSS of missing pid = %d, want 0", got)
	}
}

// TestTreeRSSCycle guards the visited set: a racing /proc read must not hang.
func TestTreeRSSCycle(t *testing.T) {
	fakeProc(t,
		map[int][]int{10: {11}, 11: {10}},
		map[int]int64{10: 1, 11: 1},
	)

	if got := TreeRSS(10); got != 2*4096 {
		t.Errorf("TreeRSS(10) = %d, want %d", got, 2*4096)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "-"},
		{-1, "-"},
		{4096, "4 KB"},
		{412 << 20, "412 MB"},
		{3 << 30, "3.0 GB"},
	}
	for _, tc := range cases {
		if got := FormatBytes(tc.in); got != tc.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
