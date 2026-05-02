#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${HOME}/.local/share/ai-attn"
BIN="${HOME}/.local/bin/ai-attn"

rm -f "$BIN"
rm -rf "$INSTALL_DIR"

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
  echo "    - Codex:       $codex_toml (notify key)"
fi

opencode_jsonc="${HOME}/.config/opencode/opencode.jsonc"
if [ -f "$opencode_jsonc" ] && grep -q "ai-attn" "$opencode_jsonc" 2>/dev/null; then
  echo "    - OpenCode:    $opencode_jsonc (plugin array)"
fi

echo "  Leftover hook entries are harmless (they exit 0 if ai-attn is missing)"
echo "  but can be removed for cleanliness."
