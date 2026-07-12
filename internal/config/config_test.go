package config

import (
	"os"
	"path/filepath"
	"testing"
)

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

	if !cfg2.Remove("my_app") {
		t.Error("Remove existing = false, want true")
	}
	if cfg2.Remove("nope") {
		t.Error("Remove missing = true, want false")
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
