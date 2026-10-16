// keepon keeps tmux sessions (and the commands inside them, like Claude Code)
// alive across reboots and power outages, restoring them automatically at boot
// via a systemd user service.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jarovkipt/keepon/internal/claudecfg"
	"github.com/jarovkipt/keepon/internal/config"
	"github.com/jarovkipt/keepon/internal/netwait"
	"github.com/jarovkipt/keepon/internal/proc"
	"github.com/jarovkipt/keepon/internal/systemd"
	"github.com/jarovkipt/keepon/internal/tmux"
)

var version = "0.2.0"

const usage = `keepon - keep tmux sessions alive across reboots

Usage:
  keepon add [path]        Register a project and start it (default: current dir)
                             --cmd "..."      command to run in the session
                                              (default: resume the project's
                                              pinned claude session, named
                                              "claude <name>")
                             --no-up          register without starting it
                             --no-autostart   don't start it at boot
  keepon remove <name...>  Unregister projects
                             --kill  also kill their tmux sessions
  keepon list              Show projects, their real state, and their RAM
  keepon attach <name>     Enter a session, starting it first if needed
  keepon autostart on|off <name...>
                           Choose which projects start at boot
  keepon up [name...]      Start sessions that aren't running
                           (default: every project with autostart on)
                             --wait-network  wait for DNS before starting
  keepon down [name...]    Kill sessions (default: all)
                             --idle  only sessions whose command already exited
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
	case "attach", "a":
		err = cmdAttach(os.Args[2:])
	case "autostart":
		err = cmdAutostart(os.Args[2:])
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

// permute reorders args so flags may follow positional arguments, the way GNU
// getopt and most CLIs behave. Go's flag package stops parsing at the first
// non-flag argument, which silently swallowed the --cmd in the documented
// "keepon add <path> --cmd ..." form and registered the project without it.
func permute(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		// A bare "-" is a conventional positional, not a flag.
		if len(arg) < 2 || arg[0] != '-' {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)

		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			continue // -name=value carries its own value
		}
		f := fs.Lookup(name)
		if f == nil {
			continue // unknown: let fs.Parse report it
		}
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue // a bool flag must not swallow the next argument
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	cmd := fs.String("cmd", "", "command to run in the session (default: resume the project's pinned claude session)")
	noUp := fs.Bool("no-up", false, "register the project without starting its session")
	noAutostart := fs.Bool("no-autostart", false, "do not start this project at boot")
	fs.Parse(permute(fs, args))

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
	if *noAutostart {
		p.SetAutostart(false)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("added %s (%s)\n", p.Name, p.Path)
	// The project is registered either way; failing to pre-accept trust only
	// means claude will stop at the prompt on restore, so warn and carry on.
	if added, err := claudecfg.EnsureTrusted([]string{p.Path}); err != nil {
		fmt.Fprintf(os.Stderr, "keepon: could not mark %s as trusted: %v\n", p.Path, err)
	} else if len(added) > 0 {
		fmt.Printf("  trusted: %s\n", p.Path)
	}
	if p.Cmd == "" {
		fmt.Printf("  claude session: %q (%s)\n", p.ClaudeSessionName(), p.SessionID)
	}
	if *noAutostart {
		fmt.Printf("  autostart: off (start it with: keepon attach %s)\n", p.Name)
	}
	if *noUp {
		fmt.Printf("  runs: %s\n", p.RunCmd())
		return nil
	}
	// Registering a project and then having to run `up` by hand is a step nobody
	// wants; adding one means you want it now.
	if failed := restore([]config.Project{*p}); failed > 0 {
		return fmt.Errorf("%s was added but its session failed to start", p.Name)
	}
	return nil
}

func cmdRemove(args []string) error {
	fs := flag.NewFlagSet("remove", flag.ExitOnError)
	kill := fs.Bool("kill", false, "also kill each project's tmux session")
	fs.Parse(permute(fs, args))

	if fs.NArg() == 0 {
		return fmt.Errorf("usage: keepon remove [--kill] <name...>")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// Resolve every name before changing anything, so a typo in the third name
	// does not leave the first two half-removed.
	for _, name := range fs.Args() {
		if cfg.Find(name) == nil {
			return fmt.Errorf("no project named %q (see: keepon list)", name)
		}
	}
	for _, name := range fs.Args() {
		if *kill && tmux.HasSession(name) {
			if err := tmux.KillSession(name); err != nil {
				return err
			}
		}
		cfg.Remove(name)
		if *kill {
			fmt.Printf("removed %s (session killed)\n", name)
		} else {
			fmt.Printf("removed %s (its tmux session, if running, was left alone)\n", name)
		}
	}
	return cfg.Save()
}

// sessionState is how much of a project is actually up.
type sessionState int

const (
	stateStopped sessionState = iota // no tmux session at all
	stateIdle                        // a session, but its pane is a bare shell
	stateRunning                     // the run command is alive in the pane
)

func (s sessionState) icon() string {
	switch s {
	case stateRunning:
		return "🟢"
	case stateIdle:
		return "🟡"
	default:
		return "⚫"
	}
}

func (s sessionState) String() string {
	switch s {
	case stateRunning:
		return "running"
	case stateIdle:
		return "idle"
	default:
		return "stopped"
	}
}

// sessionInfo is one project's live state plus what it costs in memory.
type sessionInfo struct {
	state sessionState
	rss   int64 // resident bytes of everything running in the pane
}

// inspect reports a session's real state. `tmux has-session` alone cannot tell a
// working session from one whose command exited — or was killed to free RAM —
// and left the pane sitting at a shell prompt, which is the difference between
// "leave it alone" and "start it again".
func inspect(name string) sessionInfo {
	pid, err := tmux.PanePID(name)
	if err != nil {
		return sessionInfo{state: stateStopped}
	}
	if !proc.HasChild(pid) {
		return sessionInfo{state: stateIdle}
	}
	return sessionInfo{state: stateRunning, rss: proc.TreeRSS(pid)}
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
	var running, autostart int
	var totalRSS int64
	for _, p := range cfg.Projects {
		info := inspect(p.Name)
		if info.state == stateRunning {
			running++
		}
		totalRSS += info.rss

		ram := "-"
		if info.rss > 0 {
			ram = proc.FormatBytes(info.rss)
		}
		auto := ""
		if p.AutostartEnabled() {
			autostart++
		} else {
			auto = "  (autostart off)"
		}
		fmt.Printf("%s %-8s %8s  %-28s %s%s\n",
			info.state.icon(), info.state, ram, p.Name, shortenHome(p.Path), auto)
	}
	fmt.Printf("\nrunning %d/%d · autostart %d · RAM %s\n",
		running, len(cfg.Projects), autostart, proc.FormatBytes(totalRSS))
	return nil
}

// shortenHome trims $HOME to "~" so the path column stays readable.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return path
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

func cmdAttach(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(args) != 1 {
		if err := cmdList(); err != nil {
			return err
		}
		return fmt.Errorf("usage: keepon attach <name>")
	}
	name := args[0]
	p := cfg.Find(name)
	if p == nil {
		return fmt.Errorf("no project named %q (see: keepon list)", name)
	}
	// Starting on demand is the point: projects with autostart off spend most of
	// their life stopped, and attaching is how you wake one without also paying
	// for the other eighteen.
	if inspect(p.Name).state != stateRunning {
		if failed := restore([]config.Project{*p}); failed > 0 {
			return fmt.Errorf("%s failed to start", p.Name)
		}
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	// Hand the terminal over rather than sitting in the middle of it: tmux owns
	// the tty from here, and there is nothing left for keepon to do.
	argv := tmux.AttachArgs(p.Name, os.Getenv("TMUX") != "")
	return syscall.Exec(tmuxBin, argv, os.Environ())
}

func cmdAutostart(args []string) error {
	if len(args) < 2 || (args[0] != "on" && args[0] != "off") {
		return fmt.Errorf("usage: keepon autostart on|off <name...>")
	}
	on := args[0] == "on"
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	for _, name := range args[1:] {
		if cfg.Find(name) == nil {
			return fmt.Errorf("no project named %q (see: keepon list)", name)
		}
	}
	changed := 0
	for _, name := range args[1:] {
		if cfg.Find(name).SetAutostart(on) {
			changed++
		}
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("autostart %s for %d project(s) (%d already were)\n",
		args[0], changed, len(args)-1-changed)
	if !on {
		fmt.Println("their sessions are untouched; wake one later with: keepon attach <name>")
	}
	return nil
}

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	waitNet := fs.Bool("wait-network", false, "wait for DNS resolution before starting sessions")
	fs.Parse(permute(fs, args))

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
	// Naming a project means you want that one up whatever its autostart says;
	// the setting only decides what a bare `keepon up` — which is what the boot
	// service runs — brings back.
	if fs.NArg() == 0 {
		var wanted []config.Project
		for _, p := range projects {
			if p.AutostartEnabled() {
				wanted = append(wanted, p)
			}
		}
		if skipped := len(projects) - len(wanted); skipped > 0 {
			fmt.Printf("autostart off for %d project(s), leaving them stopped\n", skipped)
		}
		projects = wanted
	}
	if len(projects) == 0 {
		fmt.Println("nothing to start — every project has autostart off")
		return nil
	}
	// Pre-accept the folder-trust prompt for every project about to start:
	// nobody is attached to these panes to answer it. One read/write covers the
	// whole batch, and it is a no-op once the paths are already trusted.
	paths := make([]string, 0, len(projects))
	for _, p := range projects {
		paths = append(paths, p.Path)
	}
	if _, err := claudecfg.EnsureTrusted(paths); err != nil {
		fmt.Fprintf(os.Stderr, "keepon: could not mark projects as trusted: %v\n", err)
	}
	if *waitNet {
		fmt.Printf("waiting for network (%s)...\n", cfg.NetworkCheckHost)
		if err := netwait.Wait(cfg.NetworkCheckHost, 90*time.Second); err != nil {
			fmt.Fprintln(os.Stderr, "keepon:", err, "- starting sessions anyway")
		}
	}
	if failed := restore(projects); failed > 0 {
		return fmt.Errorf("%d project(s) failed to start", failed)
	}
	return nil
}

// restore starts every project that is not already running and reports how many
// failed, printing one line per project in the vocabulary `keepon up` has always
// used: skip / up / warn / fail.
func restore(projects []config.Project) (failed int) {
	for _, p := range projects {
		if inspect(p.Name).state == stateRunning {
			fmt.Printf("skip  %s (already running)\n", p.Name)
			continue
		}
		blocker, err := start(p)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "fail  %s: %v\n", p.Name, err)
			failed++
		case blocker != "":
			// Started, but waiting on something only a human can answer.
			fmt.Printf("warn  %s: %s\n", p.Name, blocker)
		default:
			fmt.Printf("up    %s (%s)\n", p.Name, p.RunCmd())
		}
	}
	return failed
}

// start brings one project's session to running. A stopped project gets a fresh
// tmux session; an idle one gets the run command typed into the pane it already
// has, which is what a session whose command was killed by hand needs — without
// it the empty session would sit there forever, since it still answers
// `tmux has-session`. It returns the pane's blocker text when the command
// started but is waiting on an answer only a human can give.
func start(p config.Project) (blocker string, err error) {
	if _, err := os.Stat(p.Path); err != nil {
		return "", err
	}
	if p.Cmd == "" && p.SessionID != "" {
		// A pinned session whose claude exited before its first message can
		// neither be resumed nor created again until its stub is moved aside.
		cleared, err := claudecfg.ClearEmptyTranscript(p.SessionID)
		if err != nil {
			return "", fmt.Errorf("checking session transcript: %w", err)
		}
		if cleared {
			fmt.Printf("note  %s: moved aside an empty session transcript (no messages)\n", p.Name)
		}
	}
	if !tmux.HasSession(p.Name) {
		if err := tmux.NewSession(p.Name, p.Path); err != nil {
			return "", err
		}
	}
	if err := tmux.SendKeys(p.Name, p.RunCmd()); err != nil {
		return "", err
	}
	if !waitForStart(p.Name) {
		return "", fmt.Errorf("command did not start within %s", startTimeout)
	}
	blocker = paneBlocker(p.Name)
	// paneBlocker has given the command a couple of seconds; one that already
	// gave up (a resume that cannot find its session, say) left a bare shell
	// behind, and that is a failure, not a session that is up.
	if inspect(p.Name).state != stateRunning {
		return "", fmt.Errorf("command exited right after starting (see: tmux attach -t %s)", p.Name)
	}
	return blocker, nil
}

// startTimeout is how long to wait for the session's command to replace the
// shell in its pane before calling the start a failure.
const startTimeout = 15 * time.Second

// waitForStart reports whether the session's command is actually running. It
// asks whether the pane's shell has spawned a child rather than scraping the
// screen, so it does not depend on how any particular TUI renders itself.
func waitForStart(name string) bool {
	deadline := time.Now().Add(startTimeout)
	for {
		if pid, err := tmux.PanePID(name); err == nil && proc.HasChild(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// blockerPatterns are lowercase markers of a pane stuck waiting for a human.
// They must be specific enough not to match healthy chrome: a bare "/login"
// would flag the routine "your login expires in N days · run /login to renew"
// notice that Claude Code shows on a perfectly good session.
var blockerPatterns = []string{
	"do you trust",
	"invalid api key",
	"please run /login",
	"login required",
	"session expired",
}

// paneBlocker returns the first visible line suggesting the session is waiting
// on input nobody is there to give, or "" if it looks healthy.
func paneBlocker(name string) string {
	// The pane command flips as soon as the process execs, which is before its
	// TUI has painted anything. Give it a moment or we capture a blank screen.
	time.Sleep(2 * time.Second)
	text, err := tmux.CapturePane(name)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		for _, pat := range blockerPatterns {
			if strings.Contains(lower, pat) {
				return strings.TrimSpace(line)
			}
		}
	}
	return ""
}

func cmdDown(args []string) error {
	fs := flag.NewFlagSet("down", flag.ExitOnError)
	idleOnly := fs.Bool("idle", false, "only kill sessions whose command already exited")
	fs.Parse(permute(fs, args))

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	projects, err := selectProjects(cfg, fs.Args())
	if err != nil {
		return err
	}
	for _, p := range projects {
		state := inspect(p.Name).state
		if state == stateStopped {
			fmt.Printf("skip  %s (not running)\n", p.Name)
			continue
		}
		if *idleOnly && state != stateIdle {
			fmt.Printf("skip  %s (still running)\n", p.Name)
			continue
		}
		// Killing the session kills everything in its panes, which is the point:
		// an idle session costs nothing but a running claude costs ~400 MB.
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
	running, idle, autostart := 0, 0, 0
	var totalRSS int64
	for _, p := range cfg.Projects {
		info := inspect(p.Name)
		switch info.state {
		case stateRunning:
			running++
		case stateIdle:
			idle++
		}
		totalRSS += info.rss
		if p.AutostartEnabled() {
			autostart++
		}
	}
	fmt.Printf("sessions running: %d/%d (%d idle)\n", running, len(cfg.Projects), idle)
	fmt.Printf("start at boot: %d\n", autostart)
	fmt.Printf("RAM in sessions: %s\n", proc.FormatBytes(totalRSS))
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
