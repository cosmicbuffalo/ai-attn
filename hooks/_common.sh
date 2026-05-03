#!/usr/bin/env bash
# Common preamble for ai-attn hook scripts.
# Sourced by claude.sh, codex.sh, opencode.sh.

AI_ATTN_BIN="${AI_ATTN_BIN:-ai-attn}"
AI_ATTN_LOG="${AI_ATTN_LOG:-${HOME}/.local/state/ai-attn/hook.log}"

# Set up error logging — fall back to /dev/null if log dir doesn't exist
log_dir="$(dirname "$AI_ATTN_LOG")"
if [ -d "$log_dir" ]; then
  # Truncate log if it exceeds ~5 MB to prevent unbounded growth.
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

if ! command -v "$AI_ATTN_BIN" >/dev/null 2>&1; then
  echo "$(date -u +%FT%TZ) ${AI_ATTN_AGENT:-unknown}: ai-attn not found in PATH" >&2
  exit 0
fi
