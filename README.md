# ai-attn

Minimal attention signal layer for AI CLIs (Claude Code, Codex, OpenCode), designed to feed tmux status bars, notification daemons, or any external consumer.

AI coding agents spend most of their time working autonomously, but occasionally get stuck waiting for a human, whether for a permission prompt, a question, or just signaling they're done and ready for their next task. Without a notification layer, you're stuck watching the terminal or worrying that your agent is blocked while you're not looking. ai-attn is the first part of my attempt at addressing this problem. It allows your agent to write out signals with hook events and exposed them through a normalized and predictable interface so that you can build the rest of your notification layer on top of it using whatever tools work best for your workflow. Hook it up to tmux, or a telegram bot or whatever and stop stressing about not babysitting your agents closely enough.

## How It Works

```
AI Agent (Claude/Codex/OpenCode)
    │
    │  hook event (JSON on stdin or argv)
    ▼
Hook Script (claude.sh / codex.sh / opencode.sh)
    │
    │  ai-attn hook --agent claude/codex
    ▼
ai-attn binary
    │
    │  writes per-session JSON state files
    ▼
~/.local/state/ai-attn/<session-key>.json
    │
    │  consumer watches for filesystem changes, or polls ai-attn list --json
    ▼
tmux status bar / notification daemon / etc.
```

When an AI agent needs your attention (permission prompt, elicitation dialog, turn complete), the hook script tells `ai-attn` to record a "waiting" signal. When you respond, the hook clears it. Downstream consumers can either watch the filesystem at that ai-attn state location or poll `ai-attn list --json` to know which sessions need attention.

## Install

```bash
# One-line install (downloads pre-built binary, no Go required):
curl -fsSL https://raw.githubusercontent.com/cosmicbuffalo/ai-attn/main/install.sh | bash

# Or build from source, requires Go 1.22+:
git clone https://github.com/cosmicbuffalo/ai-attn.git
cd ai-attn
make install

# Verify:
ai-attn doctor
```

To upgrade, re-run the install script. It will prompt before overwriting, or pass `--force` to skip the prompt:

```bash
# Upgrade via curl (--force is required because piped stdin can't prompt):
curl -fsSL https://raw.githubusercontent.com/cosmicbuffalo/ai-attn/main/install.sh | bash -s -- --force
```

**Note:** The binary is installed to `~/.local/bin/`. If this directory is not in your `PATH`, the install script will print instructions to add it.

### Wiring Hooks

`install.sh` runs `ai-attn setup` automatically after installing the binary. Setup auto-detects which agents are installed by checking for their config directories (`~/.claude`, `~/.codex`, `~/.config/opencode`) and wires hooks into those found. Re-running `ai-attn setup` is idempotent — existing ai-attn entries are removed and re-added fresh, so other settings and non-ai-attn hooks are preserved.

If setup didn't run (e.g. the agent was installed after ai-attn) or only some agents were detected, you can run it yourself:

```bash
ai-attn setup              # auto-detect installed agents
ai-attn setup claude       # set up one agent explicitly
ai-attn setup --dry-run    # show what would change without writing
```

If you'd rather wire hooks by hand, [AGENTS.md](AGENTS.md) has the explicit per-agent steps — those steps can also be handed to an AI agent to perform on your behalf.

### Setting up Downstream Consumers

Once hooks are wired up, `ai-attn` writes one JSON file per session into its state directory (default: `~/.local/state/ai-attn`, overridable via `AI_ATTN_STATE_DIR`). A "downstream consumer" is anything that reads those records and turns them into something visible — a tmux status bar segment, a desktop notification, a Telegram message, an LED on your desk, whatever fits your workflow.

There are two common patterns:

- **Poll** — call `ai-attn list --json` on an interval and render the result. Easiest to wire up; latency is bounded by your poll interval. Good fit for tmux status bars, which already redraw on a timer.
- **Watch** — use `inotifywait` (Linux) or `fswatch` (macOS) on `$AI_ATTN_STATE_DIR` and react to writes. Lower latency and no idle polling, at the cost of a long-running process. Good fit for notification daemons that should fire the instant an agent starts waiting.

For per-pane checks (e.g. "is *this* tmux pane's agent waiting?"), `ai-attn status` is cheaper than parsing JSON — it exits `1` if waiting, `0` otherwise, so it composes naturally with shell conditionals (e.g. `ai-attn status ... || notify-send "agent waiting"`).

#### `list --json` schema

Polling consumers parse the output of `ai-attn list --json`:

```json
{
  "version": 1,
  "generated_at": 1710000000,
  "ttl_seconds": 259200,
  "records": [
    {
      "version": 1,
      "agent": "codex",
      "session_key": "abc123def456",
      "state": "waiting",
      "reason": "permission_prompt",
      "updated_at": 1710000000,
      "age_seconds": 2,
      "cwd": "/work/project",
      "session_id": "codex-session",
      "pane_id": "%3"
    }
  ]
}
```

Each `set-state` call refreshes `updated_at`, so `age_seconds` reflects how long the agent has been in its current state. Notable fields:

| Field | Description |
|-------|-------------|
| `state` | Current state: `waiting`, `working`, `done`, `stopped`, or empty when cleared. **This is the field consumers should key on.** |
| `reason` | Agent-specific reason for the state change (e.g., `permission_prompt`, `agent-turn-complete`). |
| `age_seconds` | Seconds since `updated_at`. |
| `pane_id` | Auto-populated from `$TMUX_PANE` when the hook runs inside tmux. For relay records (e.g., from [pmux](https://github.com/cosmicbuffalo/pmux)), this is the outer pane where attention should be displayed. |

#### Example: tmux status bar indicator

This adds a segment to your tmux status bar that shows a `⚠` when any tracked agent is waiting, and the count of waiting sessions:

```bash
# ~/.local/bin/ai-attn-tmux
#!/usr/bin/env bash
count=$(ai-attn list --json 2>/dev/null \
  | jq '[.records[] | select(.state == "waiting")] | length')
[ "${count:-0}" -gt 0 ] && printf '#[fg=yellow]⚠ %s#[default]' "$count"
```

```tmux
# ~/.tmux.conf
set -g status-interval 2
set -ag status-right '#(~/.local/bin/ai-attn-tmux)'
```

If you set `AI_ATTN_STATE_DIR` to a non-default location, make sure the tmux server inherits it (e.g. export it before `tmux new-session`, or set it via `tmux set-environment -g`) — otherwise the status script will read the default dir and see nothing.

### Test Your Setup

After wiring the hooks, verify everything works:

```bash
# Quick end-to-end test — fires a permission signal, switches to done, then clears:
ai-attn test

# Or test manually:
echo '{"hook_event_name":"Notification","notification_type":"permission_prompt","session_id":"test","cwd":"/tmp"}' \
  | ai-attn hook --agent claude
ai-attn list
echo '{"hook_event_name":"UserPromptSubmit","session_id":"test","cwd":"/tmp"}' \
  | ai-attn hook --agent claude
ai-attn list
```

## API Reference

### `ai-attn help`

Show usage and list all commands.

### `ai-attn doctor`

Check installation health: config file, state directory, hook scripts. Exits `0` if all checks pass, `1` if any check fails.

### `ai-attn version`

Print the version string.

### `ai-attn list [--json]`

List all tracked sessions. With `--json`, output machine-readable JSON (see [`list --json` schema](#list---json-schema)).

### `ai-attn logs [-f] [-n <lines>]`

Show recent hook event log entries. `-f` follows the log (like `tail -f`). `-n` sets the number of lines (default: 20).

### `ai-attn clear [--pane]`

Clear all waiting signals. With `--pane`, clear only the signal for the current tmux pane (`$TMUX_PANE`).

### `ai-attn test`

Fire a test signal cycle (permission → done → clear) to verify downstream consumer behavior is set up correctly.

### `ai-attn gc`

Remove stale state files. This runs automatically when `list` or `status` is called, but can also be triggered manually.

### `ai-attn init-config`

Create the default config file at `~/.config/ai-attn/config.toml` if it doesn't exist. Optional — `ai-attn` runs fine without a config file (defaults apply); use this only when you plan to override defaults.

### `ai-attn setup [--dry-run] [agent]`

Install ai-attn hooks into an agent's config file. With no argument, auto-detects installed agents (`claude`, `codex`, `opencode`) by checking for their config directories and sets up those found. Supported explicit subjects:

- `claude` — writes 10 hook entries into `~/.claude/settings.json`
- `codex` — sets the `notify` array in `~/.codex/config.toml`
- `opencode` — appends the bundled plugin path to the `plugin` array in `~/.config/opencode/opencode.jsonc`

Safe to re-run: existing ai-attn entries are removed and re-added fresh on each invocation, so non-ai-attn hooks, top-level settings, and other plugins are preserved. `--dry-run` previews changes without writing.

### `ai-attn status --agent <name> --session-id <id> --cwd <dir>`

Check the state of a specific session. Exits `1` if the session is waiting, `0` if not waiting or not found, `2` on invalid flags.

### `ai-attn set-state --agent <name> --state <state> [flags]`

Record an agent state. `--state` is required and must be one of `working`, `waiting`, `done`, `stopped`. Typically called by hook scripts, not directly.

Flags: `--agent`, `--state`, `--cwd`, `--session-id`, `--pane-id`, `--reason`.

### `ai-attn clear-state --agent <name> [flags]`

Clear the recorded state for a session (writes an empty state). Same flags as `set-state`.

### `ai-attn hook --agent <name>`

Process a hook event end-to-end. Reads the event payload from stdin (JSON for Claude Code) or argv (for Codex). This is the command invoked by the hook scripts — not typically called directly.

### Exit Codes

Most commands exit `0` on success and `1` on error. Notable exceptions:

| Command | Exit 0 | Exit 1 | Exit 2 |
|---------|--------|--------|--------|
| `status` | Session is not waiting (or not found) | Session is waiting | Invalid flags |
| `doctor` | All checks pass | One or more checks failed | - |

## Configuration

Default config: `~/.config/ai-attn/config.toml` — optional; built-in defaults apply when the file is missing.

```toml
enabled = true
ttl_seconds = 259200
```

| Field | Description |
|-------|-------------|
| `enabled` | Master switch. When `false`, `set-state` is a no-op. |
| `ttl_seconds` | Seconds before a record expires. Default: 259200 (72 hours). GC removes any record older than `ttl_seconds` regardless of state. |

## Platform Notes

- **macOS and Linux** are supported with pre-built binaries (amd64 and arm64).
- **Windows/WSL:** Not tested. Building from source with Go should work under WSL, but hooks assume a bash environment.
- **`~/.local/bin` in PATH:** Some distributions don't include this by default. The install script will warn if it's missing from your PATH.
- **Binary integrity:** Downloads are fetched over HTTPS from GitHub Releases. The install script does not currently verify checksums. If this concerns you, download the binary manually from the [releases page](https://github.com/cosmicbuffalo/ai-attn/releases) and verify it yourself.

### Uninstall

```bash
bash ~/.local/share/ai-attn/uninstall.sh          # removes binary and hook scripts
bash ~/.local/share/ai-attn/uninstall.sh --purge  # also removes state files and config
```

**Note:** The uninstall script removes the ai-attn binary and hook scripts, but does **not** remove hook wiring from your AI agent configs. You may need to manually clean up:

- **Claude Code:** Remove the ai-attn hook entries from `~/.claude/settings.json` (in the `hooks` section).
- **Codex:** Remove the ai-attn hook entries from `~/.codex/config.toml`.
- **OpenCode:** Remove the `~/.local/share/ai-attn/plugins/opencode` entry from the `plugin` array in `~/.config/opencode/opencode.jsonc`.

Leftover hook entries are harmless — the hook scripts always `exit 0` even if ai-attn is missing — but you can remove them for cleanliness.

## License

Apache 2.0 — see [LICENSE](LICENSE).
