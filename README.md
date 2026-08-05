# keepon

Keep your tmux sessions — and whatever runs inside them — alive across reboots and power outages.

## The problem

You run [Claude Code](https://claude.com/claude-code) (or anything else) inside tmux sessions on a home server, one session per project, so you can pick up your phone and continue from anywhere. Then the power goes out. Every session is gone, and you're back at a keyboard recreating them one by one:

```
tmux new -s project1  →  claude  →  detach
tmux new -s project2  →  claude  →  detach
...
```

`keepon` makes that automatic. Register your projects once, and after every boot each one gets its tmux session back with its command already running — for Claude Code it resumes the same pinned conversation right where it left off. No login required.

## Install

Requires Linux with systemd, tmux, and Go 1.22+.

```sh
go install github.com/jarovkipt/keepon@latest
```

Or from source:

```sh
git clone https://github.com/jarovkipt/keepon
cd keepon && go build -o keepon . && cp keepon ~/.local/bin/
```

## Quick start

```sh
keepon add ~/work/project1        # register and start (or cd there and just `keepon add`)
keepon add ~/work/project2
keepon install                    # systemd user service + lingering, one command
keepon attach project1            # jump into a session
```

That's it. On every boot, each session comes back with the same pinned Claude conversation running inside — see [Choosing what starts at boot](#choosing-what-starts-at-boot) once you have more projects than memory.

## One pinned session per project

Each project gets a permanent Claude session, created once and resumed forever — not just "the most recent one":

- `keepon add` generates a UUID and stores it as `session_id` in the config. The run command becomes:

  ```
  claude --resume <uuid> --remote-control || claude --session-id <uuid> --name 'claude <name>' --remote-control
  ```

- **First boot:** `--resume` finds nothing, so the fallback creates the session with that exact UUID and names it `claude <name>` (e.g. `claude test_project`).
- **Every boot after:** `--resume <uuid>` brings back the same conversation, no matter how many other Claude sessions you've started in that directory since.
- **`--remote-control`** registers the session with [Remote Control](https://code.claude.com/docs/en/remote-control), so it shows up in [claude.ai/code](https://claude.ai/code) and the Claude mobile app under its project name.
- Projects added before this feature get a `session_id` backfilled (and saved) on the next `keepon up`.
- If a pinned session ever gets corrupted, delete its `session_id` line from the config — a fresh one is generated on the next `keepon up`.

## Folder trust

Claude Code asks *"Do you trust the files in this folder?"* the first time it runs in a directory. A restored session is detached with nobody attached to answer, so claude would sit on that prompt forever and never reach the Claude app.

`keepon add` and `keepon up` therefore mark each registered project as trusted in Claude Code's own state file (`~/.claude.json`, key `projects["<path>"].hasTrustDialogAccepted`) before launching anything. Only paths you registered are touched, every other key in the file is preserved, and the write is skipped entirely once they're all trusted.

This pre-accepts the trust dialog only — it does **not** skip permission prompts for individual tools.

## Choosing what starts at boot

Every Claude session costs 350–450 MB of RAM. Past a handful of projects, starting all of them at boot is how you run a machine out of memory — and a session killed by the OOM reaper stays dead until someone notices.

So autostart is per project:

```sh
keepon autostart off frigate immich infra   # keep them registered, just not at boot
keepon attach frigate                       # start it when you actually want it
```

A project with autostart off keeps its `session_id`, so `keepon attach` resumes the same conversation whenever you come back to it. `keepon up frigate` starts it too — naming a project always wins over the setting, and only a bare `keepon up` (what the boot service runs) respects it.

## Commands

```
keepon add [path]        Register a project and start it (default: current dir)
                           --cmd "npm run dev"   run something other than claude
                           --no-up               register without starting it
                           --no-autostart        don't start it at boot
keepon remove <name...>  Unregister projects
                           --kill                also kill their tmux sessions
keepon list              Projects, their real state, and their RAM
keepon attach <name>     Enter a session, starting it first if needed
keepon autostart on|off <name...>
                         Choose which projects start at boot
keepon up [name...]      Start sessions that aren't running
                         (default: every project with autostart on)
keepon down [name...]    Kill sessions (default: all)
                           --idle                only ones whose command exited
keepon install           Install + enable boot restore (systemd user service)
keepon uninstall         Remove the service
keepon status            Service / lingering / session summary
keepon logs              What happened at the last restore
```

## Session state

`tmux has-session` can't tell a working session from one whose command exited and left the pane at a shell prompt — you get a session list full of green that means nothing. `keepon list` checks whether the pane's shell actually has a child process, and reports three states:

```
🟢 running    404 MB  m-all-new     ~/claude_workspace/m-all-new
🟡 idle            -  frigate       ~/frigate  (autostart off)
⚫ stopped         -  immich        ~/docker/immich  (autostart off)
```

- **running** — the command is alive; the RAM column is the resident size of everything in the pane
- **idle** — the tmux session is there but its command is gone (crashed, OOM-killed, or you killed it). `keepon up` relaunches into the pane it already has; `keepon down --idle` reaps them
- **stopped** — no tmux session at all

## How it works

- **Config** lives at `~/.config/keepon/config.json` — a plain list of `{name, path, cmd, session_id, autostart}`. Edit it by hand if you like; a missing `autostart` means yes.
- **`keepon up`** creates a detached tmux session per project (`tmux new-session -d -c <path>`) and types the command into it. A session that's already running is skipped and an idle one is relaunched in place, so it's safe to run anytime. It then waits up to 15 s for the command to actually start (checking whether the pane's shell spawned a child, which works for shell scripts and wrappers too) and reports what it sees:
  - `up` — running, nothing alarming on screen
  - `warn` — running but the pane looks stuck on a prompt (trust dialog, expired login); the message is the offending line
  - `fail` — nothing is running in the pane; counts as an error and makes `keepon up` exit non-zero
- **`keepon attach`** starts the session if it isn't running, then `exec`s tmux so it owns your terminal outright. From inside tmux it uses `switch-client` instead of `attach-session`, which tmux refuses when you're already in a session.
- **`keepon install`** writes a systemd *user* unit that runs `keepon up --wait-network` at boot, and enables [lingering](https://www.freedesktop.org/software/systemd/man/latest/loginctl.html#enable-linger%20USER%E2%80%A6) (`loginctl enable-linger`) so your user services start without anyone logging in.
- **`--wait-network`** polls DNS (default host: `api.anthropic.com`, configurable via `network_check_host`) for up to 90 s before starting sessions, because at boot the service can win the race against your network.

## Not just for Claude

The default command resumes the project's pinned Claude session, but `--cmd` bypasses pinning entirely and makes keepon a general "resurrect my tmux workspace" tool:

```sh
keepon add ~/work/api --cmd "docker compose up"
keepon add ~/notes    --cmd "nvim"
```

## Tips

- Set **"Power On after AC loss"** in your machine's BIOS — keepon can only help if the box turns itself back on.
- `systemctl --user status keepon` and `keepon logs` are your friends when something doesn't come back.
- Run `keepon list` when memory gets tight: the RAM column tells you which session to `keepon down`, and the total tells you what boot will cost you next time.

## License

MIT
