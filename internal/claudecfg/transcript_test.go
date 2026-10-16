package claudecfg

import (
	"os"
	"path/filepath"
	"testing"
)

const testSessionID = "dfa68516-0113-411d-88f8-4a1b450814a2"

// stub is what a claude started with --session-id leaves behind when it exits
// before anyone sends a message: metadata, no conversation.
const stub = `{"type":"custom-title","customTitle":"claude immich","sessionId":"dfa68516-0113-411d-88f8-4a1b450814a2"}
{"type":"mode","mode":"normal","sessionId":"dfa68516-0113-411d-88f8-4a1b450814a2"}
{"type":"bridge-session","sessionId":"dfa68516-0113-411d-88f8-4a1b450814a2","lastSequenceNum":0}
`

// transcript seeds a transcript for testSessionID under a throwaway config dir
// and returns its path.
func transcript(t *testing.T, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "projects", "-home-u-docker-immich")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Dir(filepath.Dir(dir)))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, testSessionID+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestClearEmptyTranscriptMovesStubAside(t *testing.T) {
	path := transcript(t, stub)
	cleared, err := ClearEmptyTranscript(testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("cleared = false, want true for a metadata-only transcript")
	}
	if exists(path) {
		t.Error("stub still in place; --session-id would still refuse the ID")
	}
	if !exists(path + ".empty") {
		t.Error("stub was not kept as .empty")
	}
}

func TestClearEmptyTranscriptKeepsConversation(t *testing.T) {
	path := transcript(t, stub+`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n")
	cleared, err := ClearEmptyTranscript(testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared || !exists(path) {
		t.Error("a transcript with a message was moved aside")
	}
}

func TestClearEmptyTranscriptKeepsUnparseable(t *testing.T) {
	path := transcript(t, stub+"{not json\n")
	cleared, err := ClearEmptyTranscript(testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared || !exists(path) {
		t.Error("a transcript keepon could not read was moved aside")
	}
}

func TestClearEmptyTranscriptMissing(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	cleared, err := ClearEmptyTranscript(testSessionID)
	if err != nil || cleared {
		t.Errorf("got (%v, %v), want (false, nil) when no transcript exists", cleared, err)
	}
}
