#!/usr/bin/env bash
set -euo pipefail
export AI_ATTN_AGENT="codex"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

# Codex lifecycle hooks pass a JSON payload on stdin. Bound the read so a
# caller that leaves the pipe open cannot consume the entire hook deadline.
# Older notify-style wiring passed a plain event string as argv[1], so keep
# that fallback for upgraded installs that still have stale config.
if [ -t 0 ]; then
  payload=""
else
  read_timeout="${AI_ATTN_CODEX_STDIN_TIMEOUT_SECONDS:-1}"
  if ! [[ "$read_timeout" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
    read_timeout=1
  fi
  payload=""
  IFS= read -r -d '' -t "$read_timeout" payload || true
fi
if [ -z "${payload//[[:space:]]/}" ]; then
  payload="${1:-agent-turn-complete}"
fi

printf '%s\n' "$payload" | "$AI_ATTN_BIN" hook --agent codex || true
exit 0
