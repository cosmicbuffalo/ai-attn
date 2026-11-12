#!/usr/bin/env bash
set -euo pipefail
export AI_ATTN_AGENT="grok"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

"$AI_ATTN_BIN" hook --agent grok || true
exit 0
