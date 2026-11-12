#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${HOME}/.local/share/ai-attn"
BIN="${HOME}/.local/bin/ai-attn"

rm -f "$BIN"
rm -rf "$INSTALL_DIR"

# Hook files and extensions that only ai-attn writes go with it.
for owned in \
  "${COPILOT_HOME:-${HOME}/.copilot}/hooks/ai-attn.json" \
  "${GROK_HOME:-${HOME}/.grok}/hooks/ai-attn.json" \
  "${PI_CODING_AGENT_DIR:-${HOME}/.pi/agent}/extensions/ai-attn.ts" \
  "${HOME}/.omp/agent/extensions/ai-attn.ts"; do
  if [ -f "$owned" ] && grep -q "ai-attn" "$owned" 2>/dev/null; then
    rm -f "$owned"
    echo "Removed $owned"
  fi
done

if [ "${1:-}" = "--purge" ] || [ "${1:-}" = "--purge-state" ]; then
  rm -rf "${HOME}/.local/state/ai-attn"
  rm -rf "${HOME}/.config/ai-attn"
fi

echo "Uninstalled ai-attn."
echo ""
echo "NOTE: Hook wiring in your AI agent configs was NOT removed."
echo "  You may need to manually clean up references to ai-attn in:"

settings_json="${HOME}/.claude/settings.json"
if [ -f "$settings_json" ] && grep -q "ai-attn" "$settings_json" 2>/dev/null; then
  echo "    - Claude Code: $settings_json (hooks section)"
fi

codex_toml="${HOME}/.codex/config.toml"
if [ -f "$codex_toml" ] && grep -q "ai-attn" "$codex_toml" 2>/dev/null; then
  echo "    - Codex:       $codex_toml (legacy notify key)"
fi

codex_hooks="${HOME}/.codex/hooks.json"
if [ -f "$codex_hooks" ] && grep -q "ai-attn" "$codex_hooks" 2>/dev/null; then
  echo "    - Codex:       $codex_hooks (hooks section)"
fi

cortex_hooks="${HOME}/.snowflake/cortex/hooks.json"
if [ -f "$cortex_hooks" ] && grep -q "ai-attn" "$cortex_hooks" 2>/dev/null; then
  echo "    - Cortex Code: $cortex_hooks (hooks section)"
fi

opencode_jsonc="${HOME}/.config/opencode/opencode.jsonc"
if [ -f "$opencode_jsonc" ] && grep -q "ai-attn" "$opencode_jsonc" 2>/dev/null; then
  echo "    - OpenCode:    $opencode_jsonc (plugin array)"
fi

gemini_settings="${GEMINI_CLI_HOME:-${HOME}}/.gemini/settings.json"
if [ -f "$gemini_settings" ] && grep -q "ai-attn" "$gemini_settings" 2>/dev/null; then
  echo "    - Gemini CLI:  $gemini_settings (hooks section)"
fi

agy_hooks="${HOME}/.gemini/config/hooks.json"
if [ -f "$agy_hooks" ] && grep -q "ai-attn" "$agy_hooks" 2>/dev/null; then
  echo "    - Antigravity: $agy_hooks (the \"ai-attn\" group)"
fi

if command -v muse >/dev/null 2>&1; then
  echo "    - Muse:        'muse plugins remove ai-attn' (installed plugin)"
fi

echo "  Leftover hook entries are harmless (they exit 0 if ai-attn is missing)"
echo "  but can be removed for cleanliness."
