# Agent Setup Instructions for ai-attn

This file contains step-by-step instructions for AI agents (Claude Code, Codex) to wire ai-attn hooks into their configuration. If you are an AI agent, follow the instructions for the relevant platform below.

## First, try `ai-attn setup`

`ai-attn setup` performs the same wiring described below automatically. Prefer it over editing config files by hand:

```bash
ai-attn setup              # auto-detect installed agents and wire all found
ai-attn setup claude       # wire a single agent
ai-attn setup --dry-run    # preview without writing
```

It is idempotent (existing ai-attn entries are removed and re-added fresh) and preserves non-ai-attn hooks, top-level settings, and other plugins. Two cases require explicit opt-in via `--force` (or manual wiring per the steps below):

- **Codex** supports only one global `notify` command, so setup refuses if `~/.codex/config.toml` already has a non-ai-attn `notify`.
- **OpenCode** setup re-emits `opencode.jsonc` as plain JSON, so it refuses when the file contains `//` or `/* */` comments. The Codex and OpenCode manual steps below remain valid alternatives in these cases.

After running setup, skip to [Post-Setup Verification](#post-setup-verification).

## Prerequisites

Before wiring hooks, ensure ai-attn is installed. Run `ai-attn doctor` to check. If the command is not found, install it:

**From a cloned repo (requires Go):**

```bash
make install
```

**Or without cloning (downloads pre-built binary, no Go required):**

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

   Each entry uses the nested matcher format with `async: true` and `timeout: 10`:

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
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ],
       "ElicitationResult": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ],
       "SessionEnd": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ],
       "PreToolUse": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ],
       "PostToolUse": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ],
       "StopFailure": [
         { "matcher": "", "hooks": [{ "type": "command", "command": "bash ~/.local/share/ai-attn/hooks/claude.sh", "timeout": 10, "async": true }] }
       ]
     }
   }
   ```

4. Write the updated JSON back to `~/.claude/settings.json`, preserving all other keys and formatting.

## Codex

Wire ai-attn into `~/.codex/config.toml` as the notification command.

### Steps

1. Read `~/.codex/config.toml`. If the file does not exist, create it.

2. Set the `notify` key at the top level to the following value, using the absolute path (do not use `~` or `$HOME` — TOML does not expand them):

   ```toml
   notify = ["bash", "<HOME>/.local/share/ai-attn/hooks/codex.sh"]
   ```

   Replace `<HOME>` with the actual value of the user's home directory. For example:

   ```toml
   # Linux
   notify = ["bash", "/home/username/.local/share/ai-attn/hooks/codex.sh"]
   # macOS
   notify = ["bash", "/Users/username/.local/share/ai-attn/hooks/codex.sh"]
   ```

3. If a `notify` line already exists, replace it. Write the file back.

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
| `file.edited`          | working       | Agent edited a file              |
| `permission.replied`   | working       | User granted permission          |
| `permission.updated`   | waiting       | Agent needs permission           |
| `session.idle`         | done          | Agent turn complete              |
| `session.error`        | stopped       | Agent encountered an error       |

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
