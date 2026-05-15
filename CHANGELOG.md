# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.2.0] - Unreleased

### Added

- `ai-attn setup` command for automated hook installation. Supported subjects: `claude` (writes 10 hook entries into `~/.claude/settings.json`), `codex` (writes the `notify` array into `~/.codex/config.toml`), and `opencode` (adds the bundled plugin path to the `plugin` array in `~/.config/opencode/opencode.jsonc`). With no argument, auto-detects installed agents by checking for their config directories and sets up those found.
- `--dry-run` flag on `ai-attn setup` to preview the planned changes without writing files.
- `--force` flag on `ai-attn setup` to override the two refusal cases below.
- JSONC-aware parsing for OpenCode configs: line/block comments and trailing commas are tolerated when reading `opencode.jsonc`.
- `PostToolUseFailure` to the Claude hook event list (was handled by the hook script but not previously registered by setup tooling).

### Safety

- `ai-attn setup codex` refuses to overwrite an existing `notify` command in `~/.codex/config.toml` that does not point at the canonical ai-attn hook. Codex supports only one global notify command, so the user must explicitly opt in to replacing it (remove the line manually, or pass `--force`).
- `ai-attn setup opencode` refuses to rewrite `~/.config/opencode/opencode.jsonc` when the file contains `//` or `/* */` comments, since the rewrite emits plain JSON and would drop them. Pass `--force` to accept the loss, or wire the plugin manually per AGENTS.md.
- The "is this entry ours?" marker check used by setup is now an exact filename match (`ai-attn/hooks/claude.sh`, `ai-attn/hooks/codex.sh`, `ai-attn/plugins/opencode`) instead of the loose `ai-attn/hooks/` substring. A user-authored fan-out wrapper that lives under `ai-attn/hooks/` (e.g. `codex-multi.sh` calling both peon-ping and our canonical hook) is no longer silently overwritten — it triggers the codex refusal path, and is preserved untouched alongside the canonical hook in claude's case.

### Changed

- `ai-attn doctor` now follows one level of wrapper. If the agent's config references a script path that is not the canonical hook, doctor reads that script and reports `installed (via wrapper at <path>)` when the script in turn references the canonical hook. Previously such setups were misreported as `not_wired`.

### Changed

- `install.sh` now calls `ai-attn setup` after installing the binary, replacing the previous "ask your AI agent to read AGENTS.md" prompt. Wiring is best-effort — if setup fails for any agent, install still completes and the user can re-run `ai-attn setup` manually.
- `ai-attn doctor` now suggests `ai-attn setup` in its output when an agent's hooks are not wired.
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

[0.2.0]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.2.0
[0.1.0]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.1.0
