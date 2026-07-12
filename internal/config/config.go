// Package config loads and saves keepon's project list from the XDG config dir.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultCmd is the command sent into each tmux session when a project's Cmd is empty.
const DefaultCmd = "claude --continue || claude"

// DefaultNetworkCheckHost is resolved to decide the network is up before starting sessions.
const DefaultNetworkCheckHost = "api.anthropic.com"

type Project struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Cmd  string `json:"cmd,omitempty"`
}

// RunCmd returns the command to run in the project's session.
func (p Project) RunCmd() string {
	if p.Cmd != "" {
		return p.Cmd
	}
	return DefaultCmd
}

type Config struct {
	NetworkCheckHost string    `json:"network_check_host"`
	Projects         []Project `json:"projects"`
}

// Path returns the config file location, honoring XDG_CONFIG_HOME.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "keepon", "config.json"), nil
}

// Load reads the config file; a missing file yields an empty config.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	cfg := &Config{NetworkCheckHost: DefaultNetworkCheckHost}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if cfg.NetworkCheckHost == "" {
		cfg.NetworkCheckHost = DefaultNetworkCheckHost
	}
	return cfg, nil
}

// Save writes the config file, creating the directory if needed.
func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Find returns the project with the given name, or nil.
func (c *Config) Find(name string) *Project {
	for i := range c.Projects {
		if c.Projects[i].Name == name {
			return &c.Projects[i]
		}
	}
	return nil
}

// Add appends a project derived from dir. The session name is the directory's
// basename, sanitized for tmux.
func (c *Config) Add(dir, cmd string) (*Project, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	name := SessionName(filepath.Base(abs))
	if existing := c.Find(name); existing != nil {
		return nil, fmt.Errorf("project %q already exists (%s)", name, existing.Path)
	}
	c.Projects = append(c.Projects, Project{Name: name, Path: abs, Cmd: cmd})
	return &c.Projects[len(c.Projects)-1], nil
}

// Remove deletes the named project. It reports whether it was present.
func (c *Config) Remove(name string) bool {
	for i := range c.Projects {
		if c.Projects[i].Name == name {
			c.Projects = append(c.Projects[:i], c.Projects[i+1:]...)
			return true
		}
	}
	return false
}

// SessionName converts a directory basename into a valid tmux session name.
// tmux rejects "." and ":" in session names.
func SessionName(base string) string {
	return strings.Map(func(r rune) rune {
		if r == '.' || r == ':' {
			return '_'
		}
		return r
	}, base)
}
