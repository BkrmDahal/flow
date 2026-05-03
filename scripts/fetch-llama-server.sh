#!/usr/bin/env bash
# Bundle a self-contained llama-server + its Homebrew dylibs into the
# llamacpp package so go:embed ships them at compile time.
#
# Usage:
#   brew install llama.cpp
#   ./scripts/fetch-llama-server.sh

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST_DIR="$ROOT/backend/internal/llamacpp/bin"
DEST_LIB_DIR="$DEST_DIR/lib"
DEST_BIN="$DEST_DIR/llama-server-darwin-arm64"

mkdir -p "$DEST_DIR" "$DEST_LIB_DIR"

if [[ "$(uname -m)" != "arm64" ]]; then
  echo "error: this script currently packages Apple Silicon (arm64) binaries only." >&2
  exit 1
fi

SRC_BIN=""
for candidate in \
  "$(command -v llama-server || true)" \
  "/opt/homebrew/bin/llama-server" \
  "/usr/local/bin/llama-server"
do
  if [[ -n "$candidate" && -x "$candidate" ]]; then
    SRC_BIN="$candidate"
    break
  fi
done

if [[ -z "$SRC_BIN" ]]; then
  cat <<'EOF' >&2
error: llama-server not found.
  Install it first:
      brew install llama.cpp
  Then re-run this script.
EOF
  exit 1
fi

if ! command -v otool >/dev/null 2>&1 || ! command -v install_name_tool >/dev/null 2>&1; then
  echo "error: Xcode command line tools are required (otool and install_name_tool)." >&2
  exit 1
fi

echo "copying $SRC_BIN -> $DEST_BIN"
cp "$SRC_BIN" "$DEST_BIN"
chmod 0755 "$DEST_BIN"

declare -A copied
queue=()

homebrew_deps() {
  otool -L "$1" | awk 'NR > 1 { print $1 }' | while read -r dep; do
    case "$dep" in
      /opt/homebrew/*|/usr/local/*) [[ -f "$dep" ]] && echo "$dep" ;;
    esac
  done
}

enqueue_deps() {
  local file="$1"
  while IFS= read -r dep; do
    if [[ -z "${copied[$dep]:-}" ]]; then
      queue+=("$dep")
    fi
  done < <(homebrew_deps "$file")
}

enqueue_deps "$SRC_BIN"
while ((${#queue[@]})); do
  dep="${queue[0]}"
  queue=("${queue[@]:1}")
  if [[ -n "${copied[$dep]:-}" ]]; then
    continue
  fi
  dest="$DEST_LIB_DIR/$(basename "$dep")"
  echo "copying $dep -> $dest"
  cp "$dep" "$dest"
  chmod 0755 "$dest"
  copied[$dep]="$dest"
  enqueue_deps "$dep"
done

rewrite_file() {
  local file="$1"
  local args=()
  if [[ "$file" == "$DEST_LIB_DIR"/*.dylib ]]; then
    args+=("-id" "@rpath/$(basename "$file")")
  fi
  for dep in "${!copied[@]}"; do
    args+=("-change" "$dep" "@rpath/$(basename "$dep")")
  done
  if ((${#args[@]})); then
    install_name_tool "${args[@]}" "$file"
  fi
}

echo "rewriting install names..."
install_name_tool -add_rpath "@loader_path/../lib" "$DEST_BIN" 2>/dev/null || true
rewrite_file "$DEST_BIN"
for lib in "$DEST_LIB_DIR"/*.dylib; do
  [[ -e "$lib" ]] || continue
  rewrite_file "$lib"
done

if command -v codesign >/dev/null 2>&1; then
  echo "re-signing..."
  for lib in "$DEST_LIB_DIR"/*.dylib; do
    [[ -e "$lib" ]] || continue
    codesign --force --sign - "$lib"
  done
  codesign --force --sign - "$DEST_BIN"
fi

echo "verifying bundled dependencies..."
otool -L "$DEST_BIN" | sed 's/^/  /'
echo "ok: bundled llama-server ($(du -sh "$DEST_DIR" | cut -f1))"
