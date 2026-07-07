#!/usr/bin/env bash
set -euo pipefail
export AI_ATTN_AGENT="codex"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

# Codex lifecycle hooks pass a JSON payload on stdin. Older notify-style
# wiring passed a plain event string as argv[1], so keep that fallback for
# upgraded installs that still have stale config.
if [ -t 0 ]; then
  payload=""
else
  payload="$(cat || true)"
fi
if [ -z "${payload//[[:space:]]/}" ]; then
  payload="${1:-agent-turn-complete}"
fi

printf '%s\n' "$payload" | "$AI_ATTN_BIN" hook --agent codex || true
exit 0
