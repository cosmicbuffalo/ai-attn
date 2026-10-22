# Agent Setup Instructions for ai-attn

This file contains step-by-step instructions for AI agents (Claude Code, Codex) to wire ai-attn hooks into their configuration. If you are an AI agent, follow the instructions for the relevant platform below.

## First, try `ai-attn setup`

`ai-attn setup` performs the same wiring described below automatically. Prefer it over editing config files by hand:

```bash
ai-attn setup              # auto-detect installed agents and wire all found
ai-attn setup claude       # wire a single agent
ai-attn setup --dry-run    # preview without writing
```

It is idempotent (existing ai-attn entries are removed and re-added fresh) and preserves non-ai-attn hooks, top-level settings, and other plugins. One case requires explicit opt-in via `--force` (or manual wiring per the steps below):

- **OpenCode** setup re-emits `opencode.jsonc` as plain JSON, so it refuses when the file contains `//` or `/* */` comments. The manual steps below remain a valid alternative.

After running setup, skip to [Post-Setup Verification](#post-setup-verification).

## Prerequisites

Before wiring hooks, ensure ai-attn is installed. Run `ai-attn doctor` to check. If the command is not found, install it:

**From a cloned repo (requires Go):**

```bash
make install
```

**Or without cloning (downloads and verifies a pre-built binary, no Go required):**

```bash
curl -fsSL https://raw.githubusercontent.com/cosmicbuffalo/ai-attn/main/install.sh | bash
```

After installing, ensure `~/.local/bin` is in PATH (the install script will warn if it isn't).

## Claude Code

Wire ai-attn hooks into `~/.claude/settings.json` so that attention signals are recorded for every relevant event.

### Steps

1. Read `~/.claude/settings.json`. If the file does not exist, start with an empty JSON object `{}`.

2. Ensure the top-level `"hooks"` key exists as an object. If it already exists, preserve all existing entries.

3. For each of the following ten hook events, add the ai-attn command entry. If the event key already exists with other hook entries, **append** the ai-attn entry to the existing array — do not replace existing hooks. If an entry with the same command already exists for that event, skip it (do not duplicate).

   Each entry uses the nested matcher format with `timeout: 10`. `UserPromptSubmit`, `PreToolUse`, and `PostToolUse` run synchronously (no `async` key) so their state writes always finish before `Stop`; an async tool-use write can land after `Stop` and leave a finished session stuck at "working". Every other event uses `async: true`:

   ```json
   {
     "hooks": {
       "Notification": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ],
       "PermissionRequest": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ],
       "Elicitation": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ],
       "Stop": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ],
       "UserPromptSubmit": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10 }] }
       ],
       "ElicitationResult": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ],
       "SessionEnd": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ],
       "PreToolUse": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10 }] }
       ],
       "PostToolUse": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10 }] }
       ],
       "StopFailure": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ]
     }
   }
   ```

4. Write the updated JSON back to `~/.claude/settings.json`, preserving all other keys and formatting.

## Codex

Wire ai-attn into `~/.codex/hooks.json` using Codex lifecycle hooks. Codex
passes one JSON payload on stdin for each hook event.

### Steps

1. Read `~/.codex/hooks.json`. If the file does not exist, start with:

   ```json
   { "hooks": {} }
   ```

2. Ensure the top-level `"hooks"` key exists as an object. Preserve all existing non-ai-attn hooks.

3. For `UserPromptSubmit`, `PermissionRequest`, `PreToolUse`, `PostToolUse`, and `Stop`, append one matcher group with the ai-attn command. Use the absolute path to the installed hook script:

   ```json
   {
     "hooks": {
       "UserPromptSubmit": [
         {
           "matcher": "",
           "hooks": [
             {
               "type": "command",
               "command": "bash <HOME>/.local/share/ai-attn/hooks/codex.sh",
               "timeout": 10,
               "statusMessage": "Updating ai-attn"
             }
           ]
         }
       ],
       "PermissionRequest": [
         {
           "matcher": "",
           "hooks": [
             {
               "type": "command",
               "command": "bash <HOME>/.local/share/ai-attn/hooks/codex.sh",
               "timeout": 10,
               "statusMessage": "Updating ai-attn"
             }
           ]
         }
       ],
       "PreToolUse": [
         {
           "matcher": "",
           "hooks": [
             {
               "type": "command",
               "command": "bash <HOME>/.local/share/ai-attn/hooks/codex.sh",
               "timeout": 10,
               "statusMessage": "Updating ai-attn"
             }
           ]
         }
       ],
       "PostToolUse": [
         {
           "matcher": "",
           "hooks": [
             {
               "type": "command",
               "command": "bash <HOME>/.local/share/ai-attn/hooks/codex.sh",
               "timeout": 10,
               "statusMessage": "Updating ai-attn"
             }
           ]
         }
       ],
       "Stop": [
         {
           "matcher": "",
           "hooks": [
             {
               "type": "command",
               "command": "bash <HOME>/.local/share/ai-attn/hooks/codex.sh",
               "timeout": 10,
               "statusMessage": "Updating ai-attn"
             }
           ]
         }
       ]
     }
   }
   ```

4. Replace `<HOME>` with the actual home directory. For example:

   ```json
   "command": "bash /home/username/.local/share/ai-attn/hooks/codex.sh"
   ```

5. If `~/.codex/config.toml` contains the old ai-attn `notify` entry, remove it. Leave unrelated `notify` commands alone; they are not ai-attn hooks.

## OpenCode

Wire ai-attn into OpenCode using its plugin system. The ai-attn install includes a ready-made OpenCode plugin that listens for session events and reports attention state.

### Steps

1. Ensure ai-attn is installed (see Prerequisites above) and that `~/.local/share/ai-attn/plugins/opencode/index.mjs` exists. If it does not, re-run the installer.

2. Read `~/.config/opencode/opencode.jsonc`. If the file does not exist, create it with an empty JSON object `{}`.

3. Ensure the top-level `"plugin"` key exists as an array. If it already exists, preserve all existing entries.

4. Add the ai-attn plugin path to the `"plugin"` array. Use the absolute path to the installed plugin directory:

   ```jsonc
   {
     "plugin": [
       "<HOME>/.local/share/ai-attn/plugins/opencode"
       // ... other plugins
     ]
   }
   ```

   Replace `<HOME>` with the actual value of the user's home directory. For example:

   ```jsonc
   // Linux
   "plugin": ["/home/username/.local/share/ai-attn/plugins/opencode"]
   // macOS
   "plugin": ["/Users/username/.local/share/ai-attn/plugins/opencode"]
   ```

   If the plugin path is already present, skip this step (do not duplicate).

5. Write the updated JSONC back to `~/.config/opencode/opencode.jsonc`, preserving all other keys, comments, and formatting.

### How It Works

The plugin hooks into OpenCode's event system and maps events to ai-attn states:

| OpenCode Event         | ai-attn State | Meaning                          |
|------------------------|---------------|----------------------------------|
| `session.status` (busy)| working       | Agent is actively processing     |
| `message.part.updated` | working       | Agent is generating output       |
| `permission.replied`   | working       | User answered a permission prompt|
| `permission.asked`     | waiting       | Agent needs permission (`permission.updated` on older releases) |
| `session.status` (idle), `session.idle` | done | Agent turn complete     |
| `session.error`        | stopped       | Agent encountered an error       |
| `session.deleted`      | (cleared)     | Session record removed           |

OpenCode does not wait for plugin event handlers, so the plugin queues its hook calls and runs them one at a time in the order the events arrived. `message.part.updated` repeats are reported at most once a second per session. A `message.part.updated` write that still lands after the turn's idle is dropped; the next turn opens with `session.status` busy.

`file.edited` is not tracked: it carries no session ID. `session.created` is not tracked either, because opening a session is not work.

The plugin uses OpenCode's V1 plugin API, which current releases (1.19.x) load. OpenCode's V2 plugin system, in development on its `dev` branch, does not run V1 plugins and reads `plugins` instead of `plugin`, so the plugin will need a V2 entry point when that ships.

## Muse

Wire ai-attn into Muse using its plugin system. The ai-attn install includes a ready-made Muse plugin whose hooks report attention state.

Muse is the one supported agent that cannot be wired by editing a config file — hook capabilities are only delivered through plugins, and Muse must be told to trust them before they run.

### Steps

1. Ensure ai-attn is installed (see Prerequisites above) and that `~/.local/share/ai-attn/plugins/muse/.muse-plugin/plugin.json` exists. If it does not, re-run the installer.

2. Install the bundle. Muse copies it into its own package cache, so the source directory is read once and not linked:

   ```bash
   muse plugins install "$HOME/.local/share/ai-attn/plugins/muse" --scope user
   ```

3. Trust the plugin's hook capabilities. Until this runs, the hooks are installed but inert:

   ```bash
   muse plugins approve ai-attn
   ```

4. Verify the hooks are registered:

   ```bash
   muse plugins inspect ai-attn --json
   ```

Re-run steps 2 and 3 together whenever ai-attn is upgraded. Muse pins its trust decision to the bundle's content hash, so a new bundle needs a fresh approval.

If Muse reports that plugins are unavailable in this build, its plugin subsystem is gated off and no hook wiring is possible. Setting `MUSE_EXPERIMENTAL_PLUGINS=1` in the environment enables it on builds where the gate is present but off by default; `ai-attn setup muse` sets it automatically.

### How It Works

The plugin's hooks pipe Muse's event payload to `ai-attn hook --agent muse`. Muse emits Claude-shaped payloads — the same `hook_event_name` vocabulary and the same `session_id`/`cwd` fields — so the mapping mirrors the Claude one:

| Muse Event           | ai-attn State | Meaning                          |
|----------------------|---------------|----------------------------------|
| `UserPromptSubmit`   | working       | User submitted a prompt          |
| `PostToolUse`        | working       | Agent is running tools           |
| `PermissionRequest`  | waiting       | Agent needs permission           |
| `Notification`       | waiting       | Agent is blocked on a human      |
| `Stop`               | done          | Agent turn complete              |
| `PostToolUseFailure` | stopped       | A tool call failed               |
| `StopFailure`        | stopped       | The turn ended in failure        |
| `SessionEnd`         | (cleared)     | Session record removed           |

Two constraints shape the bundle:

- **One source file per hook.** Muse rejects a manifest that points two hook capabilities at the same script (`reuses source ... already used by`), so each event has its own launcher in `hooks/` and the shared logic lives in `hooks/_common.sh`, which is sourced rather than named as a hook command.
- **No tmux environment.** Muse runs hooks with a scrubbed environment — only `HOME`, `LANG`, `LOGNAME`, `PATH`, `PWD`, `SHELL`, `SHLVL`, `TERM`, `USER` and the plugin's own `MUSE_PLUGIN_*`/`CLAUDE_PLUGIN_*` variables survive. `TMUX` and `TMUX_PANE` do not, so Muse records carry no `pane_id` and no `socket`. This is expected: consumers route them by agent and cwd instead. In pmux this is the same path already used for detached Claude agents, so a Muse session is claimed correctly as long as no two pmux sessions run Muse in the same directory.

`PreToolUse` is deliberately not wired. `PostToolUse` already provides the working heartbeat, and hooking both would double the process spawns per tool call for no extra signal.

## Post-Setup Verification

After wiring hooks, run the following checks:

1. **Verify `~/.local/bin` is in PATH.** Run: `echo $PATH | tr ':' '\n' | grep -q "$HOME/.local/bin"`. If it is not in PATH, add the following to the user's shell profile (`~/.bashrc`, `~/.zshrc`, or equivalent):

   ```bash
   export PATH="$HOME/.local/bin:$PATH"
   ```

2. **Run diagnostics:** `ai-attn doctor`

   This checks that the binary is reachable, hooks are installed, and configuration is valid. Report any failures to the user.

## Setting Up a Downstream Consumer

After hooks are wired and verified, ai-attn is recording attention state into per-session JSON files — but nothing is *surfacing* that state yet. A "downstream consumer" reads ai-attn's records and turns them into something the user actually perceives: a tmux status bar indicator, a desktop notification, a sound, a Slack/Telegram message, etc.

The right consumer is highly dependent on the user's workflow, so the goal of this step is to help the user describe their ideal setup, then iterate on an implementation together. Do not prescribe a specific solution.

### Steps

1. Ask the user how they want to be notified when an agent needs attention. Offer a few representative options to anchor the conversation (e.g. tmux status bar indicator, desktop notification, sound, push to phone, message in a chat app), and make clear they can pick something else.

2. If the user is unsure or asks for a suggestion, probe their local environment and recommend options that fit what they already have running:

   - `echo $TMUX` — if non-empty, a tmux status bar indicator is a strong default (low effort, always visible).
   - On macOS: `osascript -e 'display notification ...'` is built-in.
   - On Linux: check for `notify-send` (libnotify) or a sound player like `paplay`/`aplay`.
   - Check PATH for chat CLIs (`slack`, `telegram-send`, etc.) if they want remote/away notifications.
   - If they're frequently AFK from the terminal, lean toward audible or remote options rather than visual ones.

3. Once they've chosen an approach, clarify the details that drive implementation: should it surface *all* waiting agents or only the current pane? How quickly does it need to react (poll interval vs. filesystem watch)? Should it differentiate by state (waiting vs done vs stopped) or just fire on `waiting`?

4. Implement using `ai-attn list --json` (for polling consumers that want the full picture) or `ai-attn status` (for per-pane checks; exits `1` when waiting, `0` otherwise). The README's "Setting up Downstream Consumers" section has the JSON schema and a worked tmux example to crib from.

5. Verify the consumer end-to-end with `ai-attn test`, which fires a permission → done → clear cycle so the user can confirm the right things light up at the right times.
