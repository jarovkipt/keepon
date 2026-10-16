package claudecfg

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// projectsDir is where Claude Code keeps one transcript per session, grouped by
// the directory each session ran in. It honors $CLAUDE_CONFIG_DIR the same way
// Path does.
func projectsDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// ClearEmptyTranscript moves aside a pinned session's transcript when it holds
// no conversation, and reports whether it did.
//
// A claude started with --session-id writes its metadata (title, mode, bridge)
// to the transcript right away, but the messages only once someone talks to it.
// If it exits before that, the stub wedges the session for good: --resume says
// "No conversation found" and --session-id says the ID "is already in use", so
// both halves of the run command fail on every start. Renaming the stub to
// <id>.jsonl.empty lets --session-id create the session afresh, and loses
// nothing — a transcript with any message in it is left alone.
func ClearEmptyTranscript(sessionID string) (bool, error) {
	dir, err := projectsDir()
	if err != nil {
		return false, err
	}
	// Session IDs are UUIDs, so globbing across project dirs finds the one file
	// without having to reproduce how Claude Code encodes a cwd into a dirname.
	matches, err := filepath.Glob(filepath.Join(dir, "*", sessionID+".jsonl"))
	if err != nil {
		return false, err
	}
	cleared := false
	for _, path := range matches {
		empty, err := hasNoMessages(path)
		if err != nil {
			return cleared, err
		}
		if !empty {
			continue
		}
		if err := os.Rename(path, path+".empty"); err != nil {
			return cleared, err
		}
		cleared = true
	}
	return cleared, nil
}

// hasNoMessages reports whether a transcript lacks any user or assistant entry.
// A transcript that does not parse is reported as having messages, so that
// keepon never moves aside something it could not read.
func hasNoMessages(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	for {
		var entry struct {
			Type string `json:"type"`
		}
		err := dec.Decode(&entry)
		if errors.Is(err, io.EOF) {
			return true, nil
		}
		if err != nil {
			return false, nil
		}
		if entry.Type == "user" || entry.Type == "assistant" {
			return false, nil
		}
	}
}
