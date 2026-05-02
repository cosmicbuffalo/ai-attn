#!/usr/bin/env bash
set -euo pipefail
export AI_ATTN_AGENT="codex"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

# Codex passes the event as argv[1] (plain string or JSON)
echo "${1:-agent-turn-complete}" | "$AI_ATTN_BIN" hook --agent codex || true
exit 0
