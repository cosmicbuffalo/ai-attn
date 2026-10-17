#!/usr/bin/env sh
# Shared body for the ai-attn Muse hook launchers.
#
# Sourced by every hooks/*.sh in this bundle. It is deliberately not named as
# any hook's own command: Muse rejects a manifest that points two hook
# capabilities at the same source file ("reuses source ... already used by"),
# so each event needs its own launcher and the shared work lives here.
#
# Muse runs hooks with a scrubbed environment — only HOME, LANG, LOGNAME,
# PATH, PWD, SHELL, SHLVL, TERM, USER and the plugin's own MUSE_PLUGIN_* /
# CLAUDE_PLUGIN_* variables survive. AI_ATTN_BIN and AI_ATTN_LOG cannot be
# exported from the parent shell the way they can for the other agents, so
# both fall back to their defaults below.
#
# Muse drops TMUX and TMUX_PANE too, so records written here carry no pane_id
# and no socket. That is expected, not a bug: pmux claims these records by
# matching agent and cwd instead of pane (see pmux's relay is_sole_agent_cwd).

AI_ATTN_BIN="${AI_ATTN_BIN:-ai-attn}"
AI_ATTN_LOG="${AI_ATTN_LOG:-${HOME}/.local/state/ai-attn/hook.log}"

# Set up error logging — fall back to /dev/null if the log dir doesn't exist.
log_dir="$(dirname "$AI_ATTN_LOG")"
if [ -d "$log_dir" ]; then
  # Truncate the log if it exceeds ~5 MB to prevent unbounded growth.
  if [ -f "$AI_ATTN_LOG" ]; then
    log_size=$(stat -c%s "$AI_ATTN_LOG" 2>/dev/null || stat -f%z "$AI_ATTN_LOG" 2>/dev/null || echo 0)
    if [ "$log_size" -gt 5242880 ] 2>/dev/null; then
      : > "$AI_ATTN_LOG" 2>/dev/null || true
    fi
  fi
  exec 2>>"$AI_ATTN_LOG"
else
  exec 2>/dev/null
fi

# Forward the event payload to ai-attn. Always exit 0: a hook that fails must
# never affect the turn.
ai_attn_report() {
  if ! command -v "$AI_ATTN_BIN" >/dev/null 2>&1; then
    echo "$(date -u +%FT%TZ) muse: ai-attn not found in PATH" >&2
    exit 0
  fi
  "$AI_ATTN_BIN" hook --agent muse || true
  exit 0
}
