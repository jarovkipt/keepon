// Package tmux shells out to the tmux binary to manage sessions.
package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
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

// output runs tmux and returns its stdout, trimmed.
func output(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// PanePID returns the PID of the shell tmux started in the session's active pane.
func PanePID(name string) (int, error) {
	out, err := output("display-message", "-p", "-t", "="+name+":", "#{pane_pid}")
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("unexpected pane pid %q: %w", out, err)
	}
	return pid, nil
}

// CapturePane returns the visible text of the session's active pane.
func CapturePane(name string) (string, error) {
	return output("capture-pane", "-p", "-t", "="+name+":")
}

// KillSession terminates the session.
func KillSession(name string) error {
	return run("kill-session", "-t", "="+name)
}

// AttachArgs returns the tmux arguments that put the caller in the named
// session. Inside an existing session tmux refuses to attach (it would nest a
// client in its own terminal), so the equivalent move there is to switch the
// current client instead.
func AttachArgs(name string, insideTmux bool) []string {
	if insideTmux {
		return []string{"tmux", "switch-client", "-t", "=" + name}
	}
	return []string{"tmux", "attach-session", "-t", "=" + name}
}
