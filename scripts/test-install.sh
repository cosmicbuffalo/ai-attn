#!/usr/bin/env bash
# Offline release installer and checksum verification smoke test.
set -euo pipefail

repo_root="$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

case "$(uname -s):$(uname -m)" in
  Linux:x86_64|Linux:amd64) platform="linux-amd64" ;;
  Linux:aarch64|Linux:arm64) platform="linux-arm64" ;;
  Darwin:x86_64|Darwin:amd64) platform="darwin-amd64" ;;
  Darwin:aarch64|Darwin:arm64) platform="darwin-arm64" ;;
  *) echo "unsupported test platform" >&2; exit 1 ;;
esac

runner_dir="$tmp_dir/runner"
release_dir="$tmp_dir/release"
source_dir="$tmp_dir/source"
fake_bin="$tmp_dir/fake-bin"
mkdir -p \
  "$runner_dir" \
  "$release_dir" \
  "$fake_bin" \
  "$source_dir/hooks" \
  "$source_dir/plugins/opencode"
cp "$repo_root/install.sh" "$runner_dir/install.sh"
cp "$repo_root/hooks/"*.sh "$source_dir/hooks/"
cp "$repo_root/plugins/opencode/index.mjs" "$source_dir/plugins/opencode/index.mjs"
cp "$repo_root/plugins/opencode/package.json" "$source_dir/plugins/opencode/package.json"
cp "$repo_root/uninstall.sh" "$source_dir/uninstall.sh"

asset="ai-attn-${platform}"
cat > "$release_dir/$asset" <<'FAKE_BINARY'
#!/usr/bin/env bash
case "${1:-}" in
  version|--version|-v) echo "ai-attn v0.3.2" ;;
  setup) exit 0 ;;
esac
FAKE_BINARY
chmod +x "$release_dir/$asset"

cat > "$fake_bin/curl" <<'FAKE_CURL'
#!/usr/bin/env bash
set -euo pipefail

destination=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) destination="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
test -n "$destination"
test -n "$url"
printf '%s\n' "$url" >> "$AI_ATTN_TEST_URL_LOG"
case "$url" in
  file://*)
    cp "${url#file://}" "$destination"
    ;;
  "https://raw.githubusercontent.com/cosmicbuffalo/ai-attn/${AI_ATTN_EXPECTED_SOURCE_REF}/"*)
    relative="${url#https://raw.githubusercontent.com/cosmicbuffalo/ai-attn/${AI_ATTN_EXPECTED_SOURCE_REF}/}"
    cp "$AI_ATTN_TEST_SOURCE_DIR/$relative" "$destination"
    ;;
  *)
    echo "unexpected installer URL: $url" >&2
    exit 22
    ;;
esac
FAKE_CURL
chmod +x "$fake_bin/curl"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$release_dir" && sha256sum "$asset" > checksums.txt)
else
  (cd "$release_dir" && shasum -a 256 "$asset" > checksums.txt)
fi

install_fixture() {
  local home="$1"
  HOME="$home" \
  PATH="$fake_bin:$PATH" \
  AI_ATTN_EXPECTED_SOURCE_REF="v0.3.2" \
  AI_ATTN_RELEASE_BASE_URL="file://$release_dir" \
  AI_ATTN_TEST_SOURCE_DIR="$source_dir" \
  AI_ATTN_TEST_URL_LOG="$tmp_dir/urls.log" \
    bash "$runner_dir/install.sh" --force
}

good_home="$tmp_dir/good-home"
install_fixture "$good_home"
test -x "$good_home/.local/share/ai-attn/bin/ai-attn"
test -L "$good_home/.local/bin/ai-attn"
test "$("$good_home/.local/bin/ai-attn" version)" = "ai-attn v0.3.2"
grep -Fq "/v0.3.2/hooks/codex.sh" "$tmp_dir/urls.log"
if grep -Fq "/main/" "$tmp_dir/urls.log"; then
  echo "latest install fetched mutable main support files" >&2
  exit 1
fi

# A modified asset must fail before installation when no source checkout exists.
printf '\ncorrupt\n' >> "$release_dir/$asset"
bad_home="$tmp_dir/bad-home"
if install_fixture "$bad_home" >"$tmp_dir/bad-install.log" 2>&1; then
  echo "installer accepted a binary with a mismatched checksum" >&2
  exit 1
fi
grep -Fq "Checksum mismatch" "$tmp_dir/bad-install.log"
test ! -e "$bad_home/.local/share/ai-attn/bin/ai-attn"

echo "installer checksum tests passed"
