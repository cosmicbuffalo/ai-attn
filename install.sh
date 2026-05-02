#!/usr/bin/env bash
set -euo pipefail

REPO="cosmicbuffalo/ai-attn"
INSTALL_DIR="${HOME}/.local/share/ai-attn"
BIN_DIR="${HOME}/.local/bin"
STATE_DIR="${HOME}/.local/state/ai-attn"
CONFIG_DIR="${HOME}/.config/ai-attn"
TARGET_BIN="${INSTALL_DIR}/bin/ai-attn"
FORCE_OVERWRITE=0
VERSION="${AI_ATTN_VERSION:-latest}"

# Detect if running from a cloned repo (hooks dir exists next to this script)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FROM_REPO=0
if [ -f "$SCRIPT_DIR/hooks/claude.sh" ] && [ -f "$SCRIPT_DIR/hooks/codex.sh" ]; then
  FROM_REPO=1
fi

PLUGIN_DIR="${INSTALL_DIR}/plugins/opencode"

if [ "${1:-}" = "--force" ]; then
  FORCE_OVERWRITE=1
fi

mkdir -p "$INSTALL_DIR/bin" "$INSTALL_DIR/hooks" "$BIN_DIR" "$STATE_DIR" "$CONFIG_DIR"

if [ -e "$TARGET_BIN" ] && [ "$FORCE_OVERWRITE" -ne 1 ]; then
  if [ ! -t 0 ]; then
    echo "Refusing to overwrite existing binary: $TARGET_BIN"
    echo "Re-run with --force to overwrite non-interactively."
    exit 1
  fi

  read -r -p "Overwrite existing ai-attn binary at $TARGET_BIN? [y/N] " reply
  case "$reply" in
    y|Y|yes|YES)
      ;;
    *)
      echo "Skipped install."
      exit 0
      ;;
  esac
fi

detect_platform() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *)
      echo "Unsupported architecture: $arch" >&2
      return 1
      ;;
  esac
  case "$os" in
    linux|darwin) ;;
    *)
      echo "Unsupported OS: $os" >&2
      return 1
      ;;
  esac
  echo "${os}-${arch}"
}

download_binary() {
  local platform="$1"
  local url tmp_bin

  if [ "$VERSION" = "latest" ]; then
    url="https://github.com/${REPO}/releases/latest/download/ai-attn-${platform}"
  else
    url="https://github.com/${REPO}/releases/download/${VERSION}/ai-attn-${platform}"
  fi

  tmp_bin="$(mktemp "${INSTALL_DIR}/bin/ai-attn.dl.XXXXXX")"
  trap 'rm -f "$tmp_bin"' RETURN

  echo "Downloading ai-attn for ${platform}..."
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$tmp_bin" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$tmp_bin" "$url"
  else
    echo "Neither curl nor wget found." >&2
    return 1
  fi
  chmod +x "$tmp_bin"
  mv "$tmp_bin" "$TARGET_BIN"
  trap - RETURN
}

build_from_source() {
  if [ "$FROM_REPO" -ne 1 ]; then
    echo "Cannot build from source: not running from a cloned repo." >&2
    return 1
  fi
  if ! command -v go >/dev/null 2>&1; then
    echo "Cannot build from source: go is not installed." >&2
    return 1
  fi
  echo "Building from source..."
  local tmp_bin
  tmp_bin="$(mktemp "${INSTALL_DIR}/bin/ai-attn.tmp.XXXXXX")"
  trap 'rm -f "$tmp_bin"' RETURN
  (
    cd "$SCRIPT_DIR"
    GOCACHE="${GOCACHE:-/tmp/ai-attn-go-cache}" \
    GOMODCACHE="${GOMODCACHE:-/tmp/ai-attn-go-mod-cache}" \
    go build -ldflags "-s -w" -o "$tmp_bin" ./cmd/ai-attn
  )
  mv "$tmp_bin" "$TARGET_BIN"
  trap - RETURN
}

verify_installed_binary() {
  [ -s "$TARGET_BIN" ] || return 1
  [ -x "$TARGET_BIN" ] || return 1
  "$TARGET_BIN" version >/dev/null 2>&1
}

# When installing from a local checkout, prefer the current source tree over
# release assets so local dev installs track the repo you are standing in.
platform="$(detect_platform)" || platform=""
installed=0

if [ "$FROM_REPO" -eq 1 ] && [ "$VERSION" = "latest" ]; then
  if build_from_source; then
    installed=1
  fi
elif [ -n "$platform" ] && download_binary "$platform"; then
  installed=1
else
  if [ "$FROM_REPO" -eq 1 ]; then
    echo "Pre-built binary not available, trying build from source..." >&2
  fi
  if build_from_source; then
    installed=1
  fi
fi

if [ "$installed" -ne 1 ]; then
  echo "" >&2
  echo "Failed to install ai-attn binary." >&2
  echo "" >&2
  echo "Options:" >&2
  echo "  1. Check https://github.com/${REPO}/releases for pre-built binaries" >&2
  echo "  2. Clone the repo and install Go, then run: bash install.sh" >&2
  exit 1
fi

if ! verify_installed_binary; then
  if [ "$FROM_REPO" -eq 1 ] && [ "$VERSION" = "latest" ]; then
    echo "Installed binary failed validation, retrying local source build..." >&2
    build_from_source
  fi
  if ! verify_installed_binary; then
    echo "Installed ai-attn binary failed validation." >&2
    exit 1
  fi
fi

# Download a file from the repo (used when not running from a clone)
download_file() {
  local dest="$1" path="$2" base_url
  if [ "$VERSION" = "latest" ]; then
    base_url="https://raw.githubusercontent.com/${REPO}/main"
  else
    base_url="https://raw.githubusercontent.com/${REPO}/${VERSION}"
  fi
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$dest" "$base_url/$path"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$base_url/$path"
  fi
}

# Install hooks and support scripts
install_scripts() {
  mkdir -p "$PLUGIN_DIR"
  if [ "$FROM_REPO" -eq 1 ]; then
    install -m 0755 "$SCRIPT_DIR/hooks/_common.sh" "$INSTALL_DIR/hooks/_common.sh"
    install -m 0755 "$SCRIPT_DIR/hooks/claude.sh" "$INSTALL_DIR/hooks/claude.sh"
    install -m 0755 "$SCRIPT_DIR/hooks/codex.sh" "$INSTALL_DIR/hooks/codex.sh"
    install -m 0755 "$SCRIPT_DIR/hooks/opencode.sh" "$INSTALL_DIR/hooks/opencode.sh"
    install -m 0644 "$SCRIPT_DIR/plugins/opencode/index.mjs" "$PLUGIN_DIR/index.mjs"
    install -m 0644 "$SCRIPT_DIR/plugins/opencode/package.json" "$PLUGIN_DIR/package.json"
    install -m 0755 "$SCRIPT_DIR/uninstall.sh" "$INSTALL_DIR/uninstall.sh"
  else
    for hook in _common.sh claude.sh codex.sh opencode.sh; do
      download_file "$INSTALL_DIR/hooks/$hook" "hooks/$hook"
      chmod +x "$INSTALL_DIR/hooks/$hook"
    done
    download_file "$PLUGIN_DIR/index.mjs" "plugins/opencode/index.mjs"
    download_file "$PLUGIN_DIR/package.json" "plugins/opencode/package.json"
    download_file "$INSTALL_DIR/uninstall.sh" "uninstall.sh"
    chmod +x "$INSTALL_DIR/uninstall.sh"
  fi
}

install_scripts
ln -sf "$TARGET_BIN" "$BIN_DIR/ai-attn"

# init-config is intentionally NOT run on install. Defaults apply when the
# config file is missing; users who want to override them can run
# `ai-attn init-config` themselves.

if [ "$VERSION" = "latest" ]; then
  AGENTS_MD_URL="https://raw.githubusercontent.com/${REPO}/main/AGENTS.md"
else
  AGENTS_MD_URL="https://raw.githubusercontent.com/${REPO}/${VERSION}/AGENTS.md"
fi

cat <<EOF
Installed ai-attn.

Binary:
  $BIN_DIR/ai-attn

Next step — wire hooks into your AI agent config:
  Ask your AI agent to read AGENTS.md and follow the instructions.
EOF

if [ "$FROM_REPO" -eq 1 ]; then
  echo "  File: $SCRIPT_DIR/AGENTS.md"
else
  echo "  URL:  $AGENTS_MD_URL"
fi

cat <<EOF

  Or wire hooks manually:

  Claude hook command:
    bash $INSTALL_DIR/hooks/claude.sh

  Codex notify snippet (~/.codex/config.toml):
    notify = ["bash", "$INSTALL_DIR/hooks/codex.sh"]

  OpenCode plugin (add to plugin array in ~/.config/opencode/opencode.jsonc):
    "$PLUGIN_DIR"

Uninstall:
  bash $INSTALL_DIR/uninstall.sh

Run diagnostics:
  ai-attn doctor
EOF

# Check if BIN_DIR is in PATH
case ":${PATH}:" in
  *":${BIN_DIR}:"*) ;;
  *)
    echo ""
    echo "NOTE: $BIN_DIR is not in your PATH."
    echo "Add it to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
    echo ""
    echo "  export PATH=\"$BIN_DIR:\$PATH\""
    ;;
esac
