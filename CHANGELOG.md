# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

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

[0.1.0]: https://github.com/cosmicbuffalo/ai-attn/releases/tag/v0.1.0
