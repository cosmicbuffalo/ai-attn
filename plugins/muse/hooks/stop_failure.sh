#!/usr/bin/env sh
# StopFailure hook: forward Muse's event payload to ai-attn.
#
# A separate launcher per event because Muse rejects a reused hook source;
# the shared logic lives in _common.sh.
set -eu

plugin_root="${MUSE_PLUGIN_ROOT:-$(dirname "$0")/..}"
# shellcheck source=_common.sh
. "$plugin_root/hooks/_common.sh"

ai_attn_report
