package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var uuidV4Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestSessionName(t *testing.T) {
	cases := map[string]string{
		"bonk":           "bonk",
		"my.app":         "my_app",
		"a:b":            "a_b",
		"find_lineman":   "find_lineman",
		"web.site:v2.0":  "web_site_v2_0",
		"home-assistant": "home-assistant",
	}
	for in, want := range cases {
		if got := SessionName(in); got != want {
			t.Errorf("SessionName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunCmdDefault(t *testing.T) {
	p := Project{Name: "x", Path: "/x"}
	if got := p.RunCmd(); got != DefaultCmd {
		t.Errorf("RunCmd() = %q, want default %q", got, DefaultCmd)
	}
	p.Cmd = "htop"
	if got := p.RunCmd(); got != "htop" {
		t.Errorf("RunCmd() = %q, want %q", got, "htop")
	}
}

func TestRunCmdPinned(t *testing.T) {
	const id = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	p := Project{Name: "test_project", Path: "/x", SessionID: id}
	want := fmt.Sprintf(
		"claude --resume %s --remote-control || claude --session-id %s --name 'claude test_project' --remote-control",
		id, id)
	if got := p.RunCmd(); got != want {
		t.Errorf("RunCmd() = %q, want %q", got, want)
	}

	// An explicit Cmd override wins over the pinned session.
	p.Cmd = "htop"
	if got := p.RunCmd(); got != "htop" {
		t.Errorf("RunCmd() with Cmd override = %q, want %q", got, "htop")
	}
}

func TestNewSessionID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		id, err := NewSessionID()
		if err != nil {
			t.Fatalf("NewSessionID: %v", err)
		}
		if !uuidV4Re.MatchString(id) {
			t.Fatalf("NewSessionID() = %q, not a v4 UUID", id)
		}
		if seen[id] {
			t.Fatalf("NewSessionID() repeated %q", id)
		}
		seen[id] = true
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"claude bonk":      "'claude bonk'",
		"plain":            "'plain'",
		"it's":             `'it'\''s'`,
		"claude $HOME `x`": "'claude $HOME `x`'",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnsureSessionIDs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Simulate a config written before session pinning existed.
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"network_check_host":"api.anthropic.com","projects":[{"name":"old","path":"/x"},{"name":"pinned","path":"/y","session_id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"}]}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load old-format config: %v", err)
	}
	if cfg.Projects[0].SessionID != "" {
		t.Fatalf("old project unexpectedly has SessionID %q", cfg.Projects[0].SessionID)
	}

	changed, err := cfg.EnsureSessionIDs()
	if err != nil {
		t.Fatalf("EnsureSessionIDs: %v", err)
	}
	if !changed {
		t.Error("EnsureSessionIDs() changed = false, want true")
	}
	if !uuidV4Re.MatchString(cfg.Projects[0].SessionID) {
		t.Errorf("backfilled SessionID = %q, not a v4 UUID", cfg.Projects[0].SessionID)
	}
	if cfg.Projects[1].SessionID != "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" {
		t.Errorf("existing SessionID was modified: %q", cfg.Projects[1].SessionID)
	}

	// Idempotent: a second call changes nothing.
	first := cfg.Projects[0].SessionID
	changed, err = cfg.EnsureSessionIDs()
	if err != nil {
		t.Fatalf("EnsureSessionIDs (2nd): %v", err)
	}
	if changed {
		t.Error("second EnsureSessionIDs() changed = true, want false")
	}
	if cfg.Projects[0].SessionID != first {
		t.Errorf("SessionID changed on second call: %q -> %q", first, cfg.Projects[0].SessionID)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() on missing file: %v", err)
	}
	if len(cfg.Projects) != 0 {
		t.Fatalf("fresh config has %d projects, want 0", len(cfg.Projects))
	}
	if cfg.NetworkCheckHost != DefaultNetworkCheckHost {
		t.Fatalf("NetworkCheckHost = %q, want default", cfg.NetworkCheckHost)
	}

	dir := t.TempDir()
	proj := filepath.Join(dir, "my.app")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := cfg.Add(proj, "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if p.Name != "my_app" {
		t.Errorf("added project name = %q, want my_app", p.Name)
	}
	if !uuidV4Re.MatchString(p.SessionID) {
		t.Errorf("added project SessionID = %q, not a v4 UUID", p.SessionID)
	}
	if _, err := cfg.Add(proj, ""); err == nil {
		t.Error("duplicate Add should fail")
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if len(cfg2.Projects) != 1 || cfg2.Projects[0].Name != "my_app" || cfg2.Projects[0].Path != proj {
		t.Fatalf("round-trip mismatch: %+v", cfg2.Projects)
	}
	if cfg2.Projects[0].SessionID != p.SessionID {
		t.Errorf("SessionID did not survive round-trip: %q -> %q", p.SessionID, cfg2.Projects[0].SessionID)
	}

	if !cfg2.Remove("my_app") {
		t.Error("Remove existing = false, want true")
	}
	if cfg2.Remove("nope") {
		t.Error("Remove missing = true, want false")
	}
}

// TestAutostartDefaultsOn is the compatibility case: every config on disk today
// predates the field, and those projects must keep starting at boot.
func TestAutostartDefaultsOn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"projects":[{"name":"legacy","path":"/tmp","session_id":"abc"}]}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Projects[0].AutostartEnabled() {
		t.Error("project from a pre-autostart config = off, want on")
	}
}

func TestSetAutostartRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &Config{Projects: []Project{{Name: "a", Path: "/tmp"}}}

	if cfg.Projects[0].SetAutostart(true) {
		t.Error("SetAutostart(true) on a default project reported a change, want none")
	}
	if !cfg.Projects[0].SetAutostart(false) {
		t.Error("SetAutostart(false) reported no change, want one")
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	cfg2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Projects[0].AutostartEnabled() {
		t.Error("autostart=false did not survive the round-trip")
	}
	if !cfg2.Projects[0].SetAutostart(true) {
		t.Error("turning autostart back on reported no change")
	}
	if !cfg2.Projects[0].AutostartEnabled() {
		t.Error("SetAutostart(true) did not re-enable")
	}
}

// TestAutostartOmittedWhenOn keeps the common case out of the config file, so
// hand-editing it stays readable.
func TestAutostartOmittedWhenOn(t *testing.T) {
	data, err := json.Marshal(Project{Name: "a", Path: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "autostart") {
		t.Errorf("default project serialized as %s, want no autostart key", data)
	}
}

func TestAddRejectsNonDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	if _, err := cfg.Add(f, ""); err == nil {
		t.Error("Add(file) should fail")
	}
	if _, err := cfg.Add(filepath.Join(t.TempDir(), "missing"), ""); err == nil {
		t.Error("Add(missing) should fail")
	}
}
