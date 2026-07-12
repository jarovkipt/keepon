// Package tmux shells out to the tmux binary to manage sessions.
package tmux

import (
	"fmt"
	"os/exec"
	"strings"
)

func run(args ...string) error {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// HasSession reports whether a session with the given name exists.
func HasSession(name string) bool {
	return exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil
}

// NewSession creates a detached session rooted at dir.
func NewSession(name, dir string) error {
	return run("new-session", "-d", "-s", name, "-c", dir)
}

// SendKeys types command + Enter into the session's active pane. The trailing
// ":" makes tmux parse "=name" as an exact session match; a bare "=name" is
// rejected as a pane target.
func SendKeys(name, command string) error {
	return run("send-keys", "-t", "="+name+":", command, "Enter")
}

// KillSession terminates the session.
func KillSession(name string) error {
	return run("kill-session", "-t", "="+name)
}
