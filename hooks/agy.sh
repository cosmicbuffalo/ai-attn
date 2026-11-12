#!/usr/bin/env bash
# Antigravity CLI drops a hook's output unless it is valid JSON for the
# event, so this always prints {}. Releases before 1.5.4 send no event name,
# so each hook passes its event as the first argument.
set -uo pipefail
export AI_ATTN_AGENT="agy"
trap 'printf "{}\n"' EXIT
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"

"$AI_ATTN_BIN" hook --agent agy --event "${1:-}" >/dev/null || true
exit 0
