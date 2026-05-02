#!/usr/bin/env bash
set -euo pipefail
export AI_ATTN_AGENT="opencode"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

# OpenCode plugin pipes event JSON via stdin
"$AI_ATTN_BIN" hook --agent opencode || true
exit 0
