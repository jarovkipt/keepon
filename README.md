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
keepon add ~/work/project1        # register projects (or cd there and just `keepon add`)
keepon add ~/work/project2
keepon install                    # systemd user service + lingering, one command
keepon up                         # start everything now
```

That's it. On every boot, all sessions come back with the same pinned Claude conversation running inside. Attach with `tmux attach -t project1` as usual.

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

## Commands

```
keepon add [path]        Register a project (default: current directory)
                           --cmd "npm run dev"   run something other than claude
keepon remove <name>     Unregister a project
keepon list              Projects and their session state
keepon up [name...]      Start sessions that aren't running (default: all)
keepon down [name...]    Kill sessions
keepon install           Install + enable boot restore (systemd user service)
keepon uninstall         Remove the service
keepon status            Service / lingering / session summary
keepon logs              What happened at the last restore
```

## How it works

- **Config** lives at `~/.config/keepon/config.json` — a plain list of `{name, path, cmd, session_id}`. Edit it by hand if you like.
- **`keepon up`** creates a detached tmux session per project (`tmux new-session -d -c <path>`) and types the command into it. Sessions that already exist are skipped, so it's safe to run anytime. It then waits up to 15 s for the command to actually start (checking whether the pane's shell spawned a child, which works for shell scripts and wrappers too) and reports what it sees:
  - `up` — running, nothing alarming on screen
  - `warn` — running but the pane looks stuck on a prompt (trust dialog, expired login); the message is the offending line
  - `fail` — nothing is running in the pane; counts as an error and makes `keepon up` exit non-zero
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

## License

MIT
