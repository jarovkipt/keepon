// Package claudecfg edits Claude Code's own state file (~/.claude.json) so that
// sessions keepon restores do not stop at the interactive folder-trust prompt.
package claudecfg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const trustKey = "hasTrustDialogAccepted"

// Path returns Claude Code's config file, honoring $CLAUDE_CONFIG_DIR.
func Path() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
}

// EnsureTrusted marks each absolute dir as trusted in Claude Code's config, so a
// claude started in a detached tmux pane with nobody attached does not sit
// forever on "Do you trust the files in this folder?". It returns the dirs it
// newly trusted, and writes nothing when they are all trusted already.
//
// Claude Code rewrites ~/.claude.json on its own schedule, so a concurrent write
// could drop this edit. The read-modify-write window is milliseconds, and the
// case that matters — restoring sessions at boot — runs before any claude is
// alive. That is good enough, not airtight.
func EnsureTrusted(dirs []string) ([]string, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	root := map[string]json.RawMessage{}
	mode := os.FileMode(0o600)
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		// Claude Code merges its own defaults back in on next start.
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
	}

	// Decode only as deep as needed: every key keepon does not understand —
	// oauthAccount, caches, per-project lastSessionId — round-trips untouched.
	projects := map[string]map[string]json.RawMessage{}
	if raw, ok := root["projects"]; ok {
		if err := json.Unmarshal(raw, &projects); err != nil {
			return nil, fmt.Errorf("parsing projects in %s: %w", path, err)
		}
	}

	var added []string
	for _, dir := range dirs {
		entry, ok := projects[dir]
		if !ok {
			entry = map[string]json.RawMessage{}
			projects[dir] = entry
		}
		var trusted bool
		if raw, ok := entry[trustKey]; ok {
			// A non-boolean value here means the file is not shaped the way we
			// expect; treat it as untrusted and overwrite.
			_ = json.Unmarshal(raw, &trusted)
		}
		if trusted {
			continue
		}
		entry[trustKey] = json.RawMessage("true")
		added = append(added, dir)
	}
	if len(added) == 0 {
		return nil, nil
	}

	encoded, err := json.Marshal(projects)
	if err != nil {
		return nil, err
	}
	root["projects"] = encoded
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(path, append(out, '\n'), mode); err != nil {
		return nil, err
	}
	return added, nil
}

// writeAtomic replaces path in one rename, so a crash mid-write cannot leave
// Claude Code with a truncated config.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".keepon-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, path)
}
