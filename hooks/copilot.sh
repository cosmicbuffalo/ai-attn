#!/usr/bin/env bash
# Copilot CLI's camelCase payloads carry no event name, so each hook passes
# its event as the first argument.
set -euo pipefail
export AI_ATTN_AGENT="copilot"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

"$AI_ATTN_BIN" hook --agent copilot --event "${1:-}" || true
exit 0
