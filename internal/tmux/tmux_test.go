package tmux

import (
	"reflect"
	"testing"
)

// TestAttachArgs pins the distinction that matters in practice: the user is
// normally already sitting inside tmux, where attach-session is rejected.
func TestAttachArgs(t *testing.T) {
	got := AttachArgs("infra", true)
	want := []string{"tmux", "switch-client", "-t", "=infra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("inside tmux: %q, want %q", got, want)
	}

	got = AttachArgs("infra", false)
	want = []string{"tmux", "attach-session", "-t", "=infra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("outside tmux: %q, want %q", got, want)
	}
}
