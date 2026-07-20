package claudecfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// sandbox points Path() at a throwaway .claude.json seeded with content, and
// returns its path. An empty content means the file does not exist.
func sandbox(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, ".claude.json")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func readBack(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	return got
}

func trustOf(t *testing.T, root map[string]any, dir string) any {
	t.Helper()
	projects, ok := root["projects"].(map[string]any)
	if !ok {
		t.Fatalf("projects missing or not an object: %#v", root["projects"])
	}
	entry, ok := projects[dir].(map[string]any)
	if !ok {
		t.Fatalf("no entry for %q; projects = %#v", dir, projects)
	}
	return entry[trustKey]
}

func TestEnsureTrustedPreservesUnknownKeys(t *testing.T) {
	path := sandbox(t, `{
	  "numStartups": 42,
	  "oauthAccount": {"emailAddress": "someone@example.com"},
	  "projects": {
	    "/old": {"hasTrustDialogAccepted": true, "lastSessionId": "abc-123"}
	  }
	}`)

	added, err := EnsureTrusted([]string{"/new"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "/new" {
		t.Fatalf("added = %v, want [/new]", added)
	}

	got := readBack(t, path)
	if got["numStartups"] != float64(42) {
		t.Errorf("numStartups = %v, want 42", got["numStartups"])
	}
	acct, ok := got["oauthAccount"].(map[string]any)
	if !ok || acct["emailAddress"] != "someone@example.com" {
		t.Errorf("oauthAccount not preserved: %#v", got["oauthAccount"])
	}
	if v := trustOf(t, got, "/new"); v != true {
		t.Errorf("/new trust = %v, want true", v)
	}
	if v := trustOf(t, got, "/old"); v != true {
		t.Errorf("/old trust = %v, want true", v)
	}
	// An untouched project keeps the keys keepon knows nothing about.
	projects := got["projects"].(map[string]any)
	if old := projects["/old"].(map[string]any); old["lastSessionId"] != "abc-123" {
		t.Errorf("lastSessionId = %v, want abc-123", old["lastSessionId"])
	}
}

func TestEnsureTrustedAlreadyTrustedIsNoOp(t *testing.T) {
	const content = `{"projects":{"/x":{"hasTrustDialogAccepted":true}}}`
	path := sandbox(t, content)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	added, err := EnsureTrusted([]string{"/x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Errorf("added = %v, want none", added)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("file was rewritten:\n got %s\nwant %s", data, content)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("mtime changed, so the file was rewritten")
	}
}

func TestEnsureTrustedCreatesMissingFile(t *testing.T) {
	path := sandbox(t, "")

	added, err := EnsureTrusted([]string{"/x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 {
		t.Fatalf("added = %v, want [/x]", added)
	}
	if v := trustOf(t, readBack(t, path), "/x"); v != true {
		t.Errorf("/x trust = %v, want true", v)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestEnsureTrustedOverwritesFalseAndBatches(t *testing.T) {
	path := sandbox(t, `{"projects":{"/a":{"hasTrustDialogAccepted":false},"/b":{"hasTrustDialogAccepted":true}}}`)

	added, err := EnsureTrusted([]string{"/a", "/b", "/c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 || added[0] != "/a" || added[1] != "/c" {
		t.Fatalf("added = %v, want [/a /c]", added)
	}
	got := readBack(t, path)
	for _, dir := range []string{"/a", "/b", "/c"} {
		if v := trustOf(t, got, dir); v != true {
			t.Errorf("%s trust = %v, want true", dir, v)
		}
	}
}

func TestEnsureTrustedMalformedFileErrorsWithoutWriting(t *testing.T) {
	const content = `{"projects": not json`
	path := sandbox(t, content)

	if _, err := EnsureTrusted([]string{"/x"}); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("file was modified: %s", data)
	}
	// The temp file must not be left behind either.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("stray files left in config dir: %v", entries)
	}
}

func TestEnsureTrustedPreservesFileMode(t *testing.T) {
	path := sandbox(t, `{"projects":{}}`)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureTrusted([]string{"/x"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("mode = %o, want 644", perm)
	}
}
