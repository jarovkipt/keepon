// keepon keeps tmux sessions (and the commands inside them, like Claude Code)
// alive across reboots and power outages, restoring them automatically at boot
// via a systemd user service.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/jarovkipt/keepon/internal/config"
	"github.com/jarovkipt/keepon/internal/netwait"
	"github.com/jarovkipt/keepon/internal/systemd"
	"github.com/jarovkipt/keepon/internal/tmux"
)

var version = "0.1.0"

const usage = `keepon - keep tmux sessions alive across reboots

Usage:
  keepon add [path]        Register a project (default: current directory)
                             --cmd "..."  command to run in the session
                                          (default: resume the project's pinned
                                          claude session, named "claude <name>")
  keepon remove <name>     Unregister a project (does not kill its session)
  keepon list              Show projects and whether their session is running
  keepon up [name...]      Start sessions that aren't running (default: all)
                             --wait-network  wait for DNS before starting
  keepon down [name...]    Kill sessions (default: all)
  keepon install           Install + enable the systemd user service so
                           sessions restore automatically at boot
  keepon uninstall         Remove the systemd user service
  keepon status            Show service, linger, and session state
  keepon logs              Show restore logs (journalctl)
  keepon version           Print version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "add":
		err = cmdAdd(os.Args[2:])
	case "remove", "rm":
		err = cmdRemove(os.Args[2:])
	case "list", "ls":
		err = cmdList()
	case "up":
		err = cmdUp(os.Args[2:])
	case "down":
		err = cmdDown(os.Args[2:])
	case "install":
		err = cmdInstall()
	case "uninstall":
		err = cmdUninstall()
	case "status":
		err = cmdStatus()
	case "logs":
		err = cmdLogs()
	case "version", "--version", "-v":
		fmt.Println("keepon", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "keepon: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "keepon:", err)
		os.Exit(1)
	}
}

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	cmd := fs.String("cmd", "", "command to run in the session (default: resume the project's pinned claude session)")
	fs.Parse(args)

	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	p, err := cfg.Add(dir, *cmd)
	if err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("added %s (%s)\n", p.Name, p.Path)
	if p.Cmd == "" {
		fmt.Printf("  claude session: %q (%s)\n", p.ClaudeSessionName(), p.SessionID)
	}
	fmt.Printf("  runs: %s\n", p.RunCmd())
	return nil
}

func cmdRemove(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: keepon remove <name>")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.Remove(args[0]) {
		return fmt.Errorf("no project named %q (see: keepon list)", args[0])
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("removed %s (its tmux session, if running, was left alone)\n", args[0])
	return nil
}

func cmdList() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(cfg.Projects) == 0 {
		fmt.Println("no projects yet — add one with: keepon add <path>")
		return nil
	}
	for _, p := range cfg.Projects {
		state := "⚫ stopped"
		if tmux.HasSession(p.Name) {
			state = "🟢 running"
		}
		session := p.SessionID
		if p.Cmd != "" {
			session = "custom cmd"
		} else if session == "" {
			session = "unpinned (run: keepon up)"
		}
		fmt.Printf("%s  %-24s %-40s %s\n", state, p.Name, p.Path, session)
	}
	return nil
}

// selectProjects returns the projects matching names, or all when names is empty.
func selectProjects(cfg *config.Config, names []string) ([]config.Project, error) {
	if len(names) == 0 {
		return cfg.Projects, nil
	}
	var out []config.Project
	for _, n := range names {
		p := cfg.Find(n)
		if p == nil {
			return nil, fmt.Errorf("no project named %q (see: keepon list)", n)
		}
		out = append(out, *p)
	}
	return out, nil
}

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	waitNet := fs.Bool("wait-network", false, "wait for DNS resolution before starting sessions")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if changed, err := cfg.EnsureSessionIDs(); err != nil {
		return err
	} else if changed {
		if err := cfg.Save(); err != nil {
			// Not fatal at boot: sessions still start, the unsaved IDs just
			// regenerate on the next run.
			fmt.Fprintf(os.Stderr, "keepon: could not save pinned session ids: %v\n", err)
		}
	}
	projects, err := selectProjects(cfg, fs.Args())
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		fmt.Println("no projects yet — add one with: keepon add <path>")
		return nil
	}
	if *waitNet {
		fmt.Printf("waiting for network (%s)...\n", cfg.NetworkCheckHost)
		if err := netwait.Wait(cfg.NetworkCheckHost, 90*time.Second); err != nil {
			fmt.Fprintln(os.Stderr, "keepon:", err, "- starting sessions anyway")
		}
	}
	var failed int
	for _, p := range projects {
		if tmux.HasSession(p.Name) {
			fmt.Printf("skip  %s (already running)\n", p.Name)
			continue
		}
		if _, err := os.Stat(p.Path); err != nil {
			fmt.Fprintf(os.Stderr, "fail  %s: %v\n", p.Name, err)
			failed++
			continue
		}
		if err := tmux.NewSession(p.Name, p.Path); err != nil {
			fmt.Fprintf(os.Stderr, "fail  %s: %v\n", p.Name, err)
			failed++
			continue
		}
		if err := tmux.SendKeys(p.Name, p.RunCmd()); err != nil {
			fmt.Fprintf(os.Stderr, "fail  %s: %v\n", p.Name, err)
			failed++
			continue
		}
		fmt.Printf("up    %s (%s)\n", p.Name, p.RunCmd())
	}
	if failed > 0 {
		return fmt.Errorf("%d project(s) failed to start", failed)
	}
	return nil
}

func cmdDown(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	projects, err := selectProjects(cfg, args)
	if err != nil {
		return err
	}
	for _, p := range projects {
		if !tmux.HasSession(p.Name) {
			fmt.Printf("skip  %s (not running)\n", p.Name)
			continue
		}
		if err := tmux.KillSession(p.Name); err != nil {
			return err
		}
		fmt.Printf("down  %s\n", p.Name)
	}
	return nil
}

func cmdInstall() error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if err := systemd.Install(bin); err != nil {
		return err
	}
	unitPath, _ := systemd.UnitPath()
	fmt.Printf("installed %s (runs: %s up --wait-network)\n", unitPath, bin)

	if hint, err := systemd.EnableLinger(); err != nil {
		fmt.Fprintf(os.Stderr, "keepon: could not enable lingering: %v\n", err)
		fmt.Fprintf(os.Stderr, "run this yourself, or sessions won't start at boot:\n  %s\n", hint)
	} else {
		fmt.Println("lingering enabled — your sessions will start at boot, no login needed")
	}
	fmt.Println("\ntry it: keepon down && systemctl --user start keepon && keepon list")
	return nil
}

func cmdUninstall() error {
	if err := systemd.Uninstall(); err != nil {
		return err
	}
	fmt.Println("service removed (lingering left as-is; disable with: loginctl disable-linger)")
	return nil
}

func cmdStatus() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	onOff := map[bool]string{true: "yes", false: "no"}
	fmt.Printf("service enabled (starts at boot): %s\n", onOff[systemd.Enabled()])
	fmt.Printf("lingering (starts without login): %s\n", onOff[systemd.LingerEnabled()])
	running := 0
	for _, p := range cfg.Projects {
		if tmux.HasSession(p.Name) {
			running++
		}
	}
	fmt.Printf("sessions running: %d/%d\n", running, len(cfg.Projects))
	if !systemd.Enabled() || !systemd.LingerEnabled() {
		fmt.Println("\nrun `keepon install` to make sessions survive reboots")
	}
	return nil
}

func cmdLogs() error {
	cmd := exec.Command("journalctl", "--user", "-u", systemd.UnitName, "--no-pager", "-n", "100")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
