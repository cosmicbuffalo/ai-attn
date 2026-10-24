# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.4.1] - 2026-10-24

### Fixed

- A finished Muse session could show as "working" until its record expired.
  Muse runs short-lived background reminder sessions during and after each
  turn, and their tool hooks opened records under their own session IDs
  that nothing ever finished. pmux routes Muse records by agent and cwd, so
  it showed the user's session as working. Tool events from those sessions
  are now ignored.
- A Muse question to the user (`question_prompt`) now shows as waiting.
  0.4.0 only counted permission prompts.
- OpenCode's last events of a turn were often lost when OpenCode exited, so
  `opencode run` sessions could stay at "working" and quitting mid-turn
  left them at "stopped". The plugin now lets queued reports finish before
  OpenCode exits, and kills a hook call that hangs.
- OpenCode's question tool now shows as waiting while it blocks on the
  user, and a `session.error` from outside any session (a plugin or skill
  that fails to load) no longer leaves a record that nothing clears.
- `ai-attn doctor` no longer lets the Muse launcher update Muse, and stops
  waiting for `muse` or `codex` at its 10-second timeout even when they leave
  a child process running.
- `ai-attn doctor` says when it could not check Codex hook trust because
  `codex` is not on PATH, instead of reporting a plain `installed`.
- `ai-attn doctor` again accepts an OpenCode plugin entry that wraps
  ai-attn's plugin, as long as the wrapper is in the "plugin" list.

## [0.4.0] - 2026-10-23

### Added

- Support Muse as a fourth agent. `ai-attn hook --agent muse` parses Muse's
  Claude-shaped hook payloads, and a new plugin bundle at
  `plugins/muse` wires the events through `muse plugins install` /
  `muse plugins approve`, which `ai-attn setup muse` drives automatically.
  Muse is auto-detected from `~/.config/muse`.

  Muse runs hooks with a scrubbed environment that drops `TMUX` and
  `TMUX_PANE`, so Muse records carry no `pane_id` or `socket` and are routed
  by agent and cwd instead. The bundle uses one launcher script per event
  because Muse rejects a manifest that reuses a hook source.

### Fixed

- OpenCode permission prompts never showed as waiting on current releases,
  which renamed `permission.updated` to `permission.asked`. Both names are
  now handled.
- OpenCode hook writes could land out of order, because OpenCode does not
  wait for plugin event handlers, so a late `message.part.updated` could
  replace `done` after the turn ended. The plugin now runs its hook calls
  one at a time in arrival order, and a `message.part.updated` write that
  lands after the turn's idle is dropped.
- OpenCode's `file.edited` carries no session ID, so each one wrote a
  separate pane-keyed record that stayed at "working". It is no longer
  tracked; `message.part.updated` already covers that activity.
- `message.part.updated` is reported at most once a second per session
  instead of starting a hook process for every part change.
- A deleted OpenCode session's record is removed instead of left at
  `done`, and opening a session (`session.created`) no longer shows as
  working before the first prompt.
- Muse's `UserPromptSubmit` and `PostToolUse` hooks now run synchronously,
  as Claude's do, so their writes finish before `Stop`. Re-run
  `ai-attn setup muse` to install the updated plugin.
- A Muse notification only means waiting when it is a permission or
  elicitation dialog. Other notification types, such as an idle reminder,
  no longer turn a finished turn into "waiting".
- A finished Claude session could stay stuck at "working" indefinitely.
  Claude Code does not wait for async hooks, so a slow async `PreToolUse`
  write could land after `Stop` and overwrite `done`, with no later event to
  correct it. `ai-attn setup claude` now installs the `UserPromptSubmit`,
  `PreToolUse`, and `PostToolUse` hooks as synchronous so their writes
  always finish before `Stop`. As a backstop, Claude and Muse tool-use
  writes that land on a record already marking the turn as over are
  dropped. Re-run `ai-attn setup claude` to update existing installs.
- `ai-attn doctor` could report "All checks passed" while no signals were
  arriving. It now checks that Codex has trusted ai-attn's hooks (Codex
  0.163+ skips untrusted hooks), that the OpenCode plugin is in the `plugin`
  list rather than commented out, that Muse's ai-attn hooks are enabled and
  trusted, and that a `CLAUDE_CONFIG_DIR` other than `~/.claude` also
  carries the hooks.
- `make install` now installs the bundled OpenCode plugin files.
  `ai-attn setup opencode` has always pointed OpenCode at
  `~/.local/share/ai-attn/plugins/opencode`, but only `install.sh` populated
  that directory, so a `make install` install wired OpenCode to a path that
  did not exist.

## [0.3.3] - 2026-08-27

### Changed

- Increase the default state-record retention TTL from 72 hours to 90 days. Existing explicit `ttl_seconds` configuration remains unchanged.
- `ai-attn doctor` now reports agents without a config directory as `not_installed` without failing diagnostics. Installed agents that are not wired still fail with a setup hint.
- `make build` now embeds the repository version so source-built binaries remain compatible with version-checking consumers.

## [0.3.2] - 2026-07-10

### Fixed

- Include the owning tmux socket in automatic session keys so the same agent session running on two tmux servers cannot overwrite one shared state file.
- Migrate compatible pre-v0.3.2 state on the next write and retain status fallback for legacy keys during upgrade.
- Fetch hook and support files from the installed binary's immutable release tag instead of mutable `main` during latest-release installs.

### Security

- Download and verify the release `checksums.txt` before installing a pre-built binary.

## [0.3.1] - 2026-07-07

### Changed

- `ai-attn setup codex` now installs current Codex lifecycle hooks in `~/.codex/hooks.json` for `UserPromptSubmit`, `PermissionRequest`, `PreToolUse`, `PostToolUse`, and `Stop`, and removes legacy ai-attn `notify` wiring from `~/.codex/config.toml` when found. Unrelated `notify` commands are preserved.
- `ai-attn doctor` now checks Codex hook wiring in `~/.codex/hooks.json` and reports old ai-attn `notify` wiring as `legacy_notify` with a setup hint.
- The bundled Codex hook script now reads JSON hook payloads from stdin, while keeping argv event-string fallback support for stale notify-style setups.

## [0.3.0] - 2026-06-24

### Added

- `socket` field on state records, plus a `--socket` flag on `set-state`/`clear-state`, recording the tmux server that owns `pane_id`. Pane IDs (`%N`) are only unique within a single tmux server, but the state directory is shared across servers, so a `%15` written by one server could be matched against an unrelated `%15` in another. Hook-driven records populate it automatically from `$TMUX`; relay producers pass it explicitly. Empty `socket` (older producers) is treated by consumers as matching any server, so behaviour degrades gracefully.

## [0.2.1] - 2026-05-27

### Fixed

- Truncate hook.log in place instead of rotating to .1

## [0.2.0] - 2026-05-16

### Added

- `ai-attn setup` command for automated hook installation. Supported subjects: `claude` (writes 11 hook entries into `~/.claude/settings.json`), `codex` (writes the `notify` array into `~/.codex/config.toml`), and `opencode` (adds the bundled plugin path to the `plugin` array in `~/.config/opencode/opencode.jsonc`). With no argument, auto-detects installed agents by checking for their config directories and sets up those found.
- `--dry-run` flag on `ai-attn setup` to preview the planned changes without writing files.
- `--force` flag on `ai-attn setup` to override the two refusal cases below.
- JSONC-aware parsing for OpenCode configs: line/block comments and trailing commas are tolerated when reading `opencode.jsonc`.
- `PostToolUseFailure` to the Claude hook event list (was handled by the hook script but not previously registered by setup tooling).

### Safety

- `ai-attn setup codex` refuses to overwrite an existing `notify` command in `~/.codex/config.toml` that does not point at the canonical ai-attn hook. Codex supports only one global notify command, so the user must explicitly opt in to replacing it (remove the line manually, or pass `--force`).
- `ai-attn setup opencode` refuses to rewrite `~/.config/opencode/opencode.jsonc` when the file contains `//` or `/* */` comments, since the rewrite emits plain JSON and would drop them. Pass `--force` to accept the loss, or wire the plugin manually per AGENTS.md.
- The "is this entry ours?" marker check used by setup is now an exact filename match (`ai-attn/hooks/claude.sh`, `ai-attn/hooks/codex.sh`, `ai-attn/plugins/opencode`) instead of the loose `ai-attn/hooks/` substring. A user-authored fan-out wrapper that lives under `ai-attn/hooks/` (e.g. `codex-multi.sh` calling both peon-ping and our canonical hook) is no longer silently overwritten — it triggers the codex refusal path, and is preserved untouched alongside the canonical hook in claude's case.

### Changed

- `install.sh` now calls `ai-attn setup` after installing the binary, replacing the previous "ask your AI agent to read AGENTS.md" prompt. Wiring is best-effort — if setup fails for any agent, install still completes and the user can re-run `ai-attn setup` manually.
- `ai-attn doctor` now suggests `ai-attn setup` in its output when an agent's hooks are not wired, and follows one level of wrapper: if the agent's config references a script path that is not the canonical hook, doctor reads that script and reports `installed (via wrapper at <path>)` when the script in turn references the canonical hook. Previously such setups were misreported as `not_wired`.
- Re-running `ai-attn setup` is idempotent for each agent: existing ai-attn entries are removed and re-added fresh, so config drift across upgrades is self-healing. Non-ai-attn hook entries, top-level settings, and other plugins are preserved.

### Note

- OpenCode config files are re-emitted as plain JSON after setup runs. Trailing commas present in the source `opencode.jsonc` are not preserved in the rewritten file. Files containing `//` or `/* */` comments are refused unless `--force` is passed (see Safety above).

## [0.1.0] - 2026-05-02

Initial public release.

### Added

- Single-binary CLI for tracking AI attention signals: `hook`, `set-state`, `clear-state`, `status`, `list`, `clear`, `logs`, `test`, `doctor`, `gc`, `init-config`, `version`, `help`
- Hook scripts for Claude Code, Codex, and OpenCode (via the bundled OpenCode plugin under `plugins/opencode/`)
- Atomic per-session JSON state files in `~/.local/state/ai-attn/`, keyed by agent, session ID, and pane ID; location overridable via `AI_ATTN_STATE_DIR`
- TOML config at `~/.config/ai-attn/config.toml` (`enabled`, `ttl_seconds`); location overridable via `AI_ATTN_CONFIG`. Default TTL is 72 hours and GC removes any record older than `ttl_seconds`
- Automatic garbage collection on `list`/`status` invocations; manual run via `ai-attn gc`
- `ai-attn status` exits `1` when the queried session is waiting and `0` otherwise, composing naturally with shell conditionals (e.g. `ai-attn status ... || notify-send ...`)
- `ai-attn list --json` machine-readable output for downstream consumers (tmux status bars, notification daemons, etc.); see README for the schema
- `ai-attn clear` with `--pane` flag for clearing only the current tmux pane
- `ai-attn logs` with `-f` (follow) and `-n` (line count) flags
- `ai-attn test` for end-to-end permission → done → clear cycle verification
- Colored, column-aligned `ai-attn list` output with agent, state, age, and cwd columns
- Sibling record clearing: clearing a session also clears stale records from cwd changes mid-session
- Support for Claude Code `Elicitation` and `ElicitationResult` hook events
- AGENTS.md with step-by-step hook wiring instructions for AI agents to follow
- Cross-platform support (Linux/macOS, amd64/arm64)
- Installer script with pre-built binary download and source build fallback
- Uninstaller script for clean removal
- CI pipeline with formatting, linting, and test checks
- Automated multi-platform release builds via GitHub Actions

[0.4.1]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.4.1
[0.4.0]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.4.0
[0.3.3]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.3.3
[0.3.2]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.3.2
[0.3.1]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.3.1
[0.3.0]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.3.0
[0.2.1]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.2.1
[0.2.0]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.2.0
[0.1.0]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.1.0
