// Package systemd installs and manages the keepon systemd user service.
package systemd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

const UnitName = "keepon.service"

const unitTemplate = `[Unit]
Description=keepon - restore tmux sessions after boot

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=%s up --wait-network

[Install]
WantedBy=default.target
`

// UnitPath returns ~/.config/systemd/user/keepon.service.
func UnitPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user", UnitName), nil
}

// UnitContent renders the unit file for the given keepon binary path.
func UnitContent(binPath string) string {
	return fmt.Sprintf(unitTemplate, binPath)
}

// Install writes the unit file, reloads systemd, and enables the service.
func Install(binPath string) error {
	path, err := UnitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(UnitContent(binPath)), 0o644); err != nil {
		return err
	}
	if err := userctl("daemon-reload"); err != nil {
		return err
	}
	return userctl("enable", UnitName)
}

// Uninstall disables the service and removes the unit file.
func Uninstall() error {
	_ = userctl("disable", UnitName)
	path, err := UnitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return userctl("daemon-reload")
}

// EnableLinger makes the user's systemd instance start at boot without login.
// Returns a hint command for the user if polkit denies the request.
func EnableLinger() (hint string, err error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	out, err := exec.Command("loginctl", "enable-linger", u.Username).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("sudo loginctl enable-linger %s", u.Username),
			fmt.Errorf("loginctl enable-linger: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return "", nil
}

// LingerEnabled reports whether lingering is on for the current user.
func LingerEnabled() bool {
	u, err := user.Current()
	if err != nil {
		return false
	}
	out, err := exec.Command("loginctl", "show-user", u.Username, "--property=Linger").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Linger=yes"
}

// Enabled reports whether the keepon service is enabled.
func Enabled() bool {
	return exec.Command("systemctl", "--user", "is-enabled", "--quiet", UnitName).Run() == nil
}

func userctl(args ...string) error {
	full := append([]string{"--user"}, args...)
	out, err := exec.Command("systemctl", full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
