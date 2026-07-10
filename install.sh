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

# Detect if running from a cloned repo (hooks dir exists next to this script).
# When piped through `curl ... | bash`, BASH_SOURCE[0] is unset because bash is
# reading the script from stdin — in that case there is no script directory and
# FROM_REPO stays 0.
FROM_REPO=0
SCRIPT_DIR=""
if [ -n "${BASH_SOURCE[0]:-}" ]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  if [ -f "$SCRIPT_DIR/hooks/claude.sh" ] && [ -f "$SCRIPT_DIR/hooks/codex.sh" ]; then
    FROM_REPO=1
  fi
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
  local base_url url checksums_url tmp_bin tmp_checksums release_tag

  if [ -n "${AI_ATTN_RELEASE_BASE_URL:-}" ]; then
    base_url="${AI_ATTN_RELEASE_BASE_URL%/}"
  elif [ "$VERSION" = "latest" ]; then
    base_url="https://github.com/${REPO}/releases/latest/download"
  else
    case "$VERSION" in
      v*) release_tag="$VERSION" ;;
      *) release_tag="v${VERSION}" ;;
    esac
    base_url="https://github.com/${REPO}/releases/download/${release_tag}"
  fi
  url="${base_url}/ai-attn-${platform}"
  checksums_url="${base_url}/checksums.txt"

  tmp_bin="$(mktemp "${INSTALL_DIR}/bin/ai-attn.dl.XXXXXX")"
  tmp_checksums="$(mktemp "${INSTALL_DIR}/bin/checksums.dl.XXXXXX")"
  trap 'rm -f "$tmp_bin" "$tmp_checksums"' RETURN

  echo "Downloading ai-attn for ${platform}..."
  if command -v curl >/dev/null 2>&1; then
    if ! curl -fsSL -o "$tmp_bin" "$url" || ! curl -fsSL -o "$tmp_checksums" "$checksums_url"; then
      return 1
    fi
  elif command -v wget >/dev/null 2>&1; then
    if ! wget -qO "$tmp_bin" "$url" || ! wget -qO "$tmp_checksums" "$checksums_url"; then
      return 1
    fi
  else
    echo "Neither curl nor wget found." >&2
    return 1
  fi
  if ! verify_download_checksum "$tmp_bin" "$tmp_checksums" "ai-attn-${platform}"; then
    return 1
  fi
  chmod +x "$tmp_bin" || return 1
  mv "$tmp_bin" "$TARGET_BIN" || return 1
  trap - RETURN
  rm -f "$tmp_checksums"
}

verify_download_checksum() {
  local file="$1" checksums_file="$2" asset="$3"
  local expected actual

  expected="$(awk -v name="$asset" '$2 == name || $2 == "*" name {print $1; exit}' "$checksums_file")"
  if [ -z "$expected" ]; then
    echo "Checksum entry not found for ${asset}." >&2
    return 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$file" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$file" | awk '{print $1}')"
  else
    echo "Neither sha256sum nor shasum found." >&2
    return 1
  fi
  if [ "$actual" != "$expected" ]; then
    echo "Checksum mismatch for ${asset}." >&2
    return 1
  fi
}

source_build_version() {
  if [ "$FROM_REPO" -eq 1 ] && [ -f "$SCRIPT_DIR/VERSION" ]; then
    local source_version
    source_version="$(tr -d '[:space:]' < "$SCRIPT_DIR/VERSION")"
    if [ -n "$source_version" ]; then
      printf 'v%s\n' "$source_version"
    fi
  fi
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
  local build_version ldflags tmp_bin
  build_version="$(source_build_version)"
  ldflags="-s -w"
  if [ -n "$build_version" ]; then
    ldflags="${ldflags} -X main.version=${build_version}"
  fi
  tmp_bin="$(mktemp "${INSTALL_DIR}/bin/ai-attn.tmp.XXXXXX")"
  trap 'rm -f "$tmp_bin"' RETURN
  (
    cd "$SCRIPT_DIR"
    GOCACHE="${GOCACHE:-/tmp/ai-attn-go-cache}" \
    GOMODCACHE="${GOMODCACHE:-/tmp/ai-attn-go-mod-cache}" \
    go build -ldflags "$ldflags" -o "$tmp_bin" ./cmd/ai-attn
  )
  mv "$tmp_bin" "$TARGET_BIN"
  trap - RETURN
}

verify_installed_binary() {
  [ -s "$TARGET_BIN" ] || return 1
  [ -x "$TARGET_BIN" ] || return 1
  "$TARGET_BIN" version >/dev/null 2>&1
}

read_ai_attn_version() {
  local bin="$1" version_output
  version_output="$("$bin" version 2>/dev/null || true)"
  version_output="${version_output#ai-attn }"
  if [ -n "$version_output" ]; then
    printf '%s\n' "$version_output"
  else
    printf 'unknown\n'
  fi
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
installed_version="$(read_ai_attn_version "$TARGET_BIN")"

# Keep downloaded hooks and support files on the same immutable release as the
# installed binary. Using mutable main here can pair an older latest-release
# binary with newer, incompatible hook payloads between releases.
if [ "$VERSION" = "latest" ]; then
  version_pattern='^v?([0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?)$'
  if [[ "$installed_version" =~ $version_pattern ]]; then
    SOURCE_REF="v${BASH_REMATCH[1]}"
  else
    echo "Cannot determine the installed release tag from: $installed_version" >&2
    exit 1
  fi
else
  case "$VERSION" in
    v*) SOURCE_REF="$VERSION" ;;
    *) SOURCE_REF="v${VERSION}" ;;
  esac
fi

# Download a file from the repo (used when not running from a clone)
download_file() {
  local dest="$1" path="$2" base_url
  if [ -n "${AI_ATTN_SOURCE_BASE_URL:-}" ]; then
    base_url="${AI_ATTN_SOURCE_BASE_URL%/}"
  else
    base_url="https://raw.githubusercontent.com/${REPO}/${SOURCE_REF}"
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

cat <<EOF
Installed ai-attn ${installed_version}.

Binary:
  $BIN_DIR/ai-attn

Wiring hooks...
EOF

"$BIN_DIR/ai-attn" setup || true

cat <<EOF

Uninstall:
  bash $INSTALL_DIR/uninstall.sh

Run diagnostics:
  ai-attn doctor
EOF

# Check if BIN_DIR is in PATH
case ":${PATH}:" in
  *":${BIN_DIR}:"*)
    resolved_bin="$(command -v ai-attn 2>/dev/null || true)"
    if [ -n "$resolved_bin" ] && [ "$resolved_bin" != "$BIN_DIR/ai-attn" ]; then
      resolved_version="$(read_ai_attn_version "$resolved_bin")"
      if [ "$resolved_version" != "$installed_version" ]; then
        cat <<EOF

NOTE: your shell currently resolves ai-attn to:
  $resolved_bin ($resolved_version)

The installer updated:
  $BIN_DIR/ai-attn ($installed_version)

Move $BIN_DIR earlier in PATH or remove the older binary if you want \`ai-attn\` to run this version.
EOF
      fi
    fi
    ;;
  *)
    echo ""
    echo "NOTE: $BIN_DIR is not in your PATH."
    echo "Add it to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
    echo ""
    echo "  export PATH=\"$BIN_DIR:\$PATH\""
    ;;
esac
