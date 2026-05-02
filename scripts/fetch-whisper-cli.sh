#!/usr/bin/env bash
# Bundle a self-contained whisper-cli + its dylibs into the speech package so
# go:embed ships them at compile time. Run this once after
# `brew install whisper-cpp`, or whenever you upgrade whisper-cpp.
#
# What it does:
#   1. Copies whisper-cli + libwhisper.1.dylib + libggml.0.dylib +
#      libggml-base.0.dylib out of /opt/homebrew.
#   2. Rewrites the absolute /opt/homebrew/... install names with
#      install_name_tool so they resolve via @rpath at runtime.
#   3. Re-signs everything ad-hoc (modifying a Mach-O invalidates its sig).
#
# Result: the binary works without /opt/homebrew on the user's machine.
#
# Usage:  ./scripts/fetch-whisper-cli.sh

set -euo pipefail

DEST_DIR="$(cd "$(dirname "$0")/.." && pwd)/backend/internal/speech/bin"
LIB_DIR="$DEST_DIR/lib"
DEST_BIN="$DEST_DIR/whisper-cli-darwin-arm64"

mkdir -p "$LIB_DIR"

if [[ "$(uname -m)" != "arm64" ]]; then
  echo "error: this script must run on Apple Silicon (arm64) — current arch: $(uname -m)" >&2
  exit 1
fi

SRC_BIN=""
for candidate in \
  "$(command -v whisper-cli || true)" \
  "/opt/homebrew/bin/whisper-cli" \
  "/usr/local/bin/whisper-cli"
do
  if [[ -n "$candidate" && -x "$candidate" ]]; then
    SRC_BIN="$candidate"
    break
  fi
done

if [[ -z "$SRC_BIN" ]]; then
  cat <<'EOF' >&2
error: whisper-cli not found.
  Install it first:
      brew install whisper-cpp
  Then re-run this script.
EOF
  exit 1
fi

# Resolve dylib source paths (follow brew symlinks).
SRC_LIBWHISPER="$(readlink -f /opt/homebrew/opt/whisper-cpp/lib/libwhisper.1.dylib 2>/dev/null || true)"
SRC_LIBGGML="$(readlink -f /opt/homebrew/opt/ggml/lib/libggml.0.dylib 2>/dev/null || true)"
SRC_LIBGGML_BASE="$(readlink -f /opt/homebrew/opt/ggml/lib/libggml-base.0.dylib 2>/dev/null || true)"

for dep in "$SRC_LIBWHISPER" "$SRC_LIBGGML" "$SRC_LIBGGML_BASE"; do
  if [[ -z "$dep" || ! -f "$dep" ]]; then
    echo "error: missing dylib dependency: $dep" >&2
    echo "       run: brew install whisper-cpp ggml" >&2
    exit 1
  fi
done

echo "copying $SRC_BIN → $DEST_BIN"
cp "$SRC_BIN" "$DEST_BIN"
chmod 0755 "$DEST_BIN"

DEST_LIBWHISPER="$LIB_DIR/libwhisper.1.dylib"
DEST_LIBGGML="$LIB_DIR/libggml.0.dylib"
DEST_LIBGGML_BASE="$LIB_DIR/libggml-base.0.dylib"

echo "copying dylibs → $LIB_DIR/"
cp "$SRC_LIBWHISPER"     "$DEST_LIBWHISPER"
cp "$SRC_LIBGGML"        "$DEST_LIBGGML"
cp "$SRC_LIBGGML_BASE"   "$DEST_LIBGGML_BASE"
chmod 0755 "$LIB_DIR"/*.dylib

# Rewrite absolute /opt/homebrew paths in the binary so they resolve via the
# rpath @loader_path/../lib (which the brew build already sets to ../lib).
echo "rewriting install names…"
install_name_tool \
  -change /opt/homebrew/opt/ggml/lib/libggml.0.dylib      @rpath/libggml.0.dylib \
  -change /opt/homebrew/opt/ggml/lib/libggml-base.0.dylib @rpath/libggml-base.0.dylib \
  "$DEST_BIN"

# libwhisper internally references the same /opt/homebrew ggml paths.
install_name_tool \
  -id   @rpath/libwhisper.1.dylib \
  -change /opt/homebrew/opt/ggml/lib/libggml.0.dylib      @rpath/libggml.0.dylib \
  -change /opt/homebrew/opt/ggml/lib/libggml-base.0.dylib @rpath/libggml-base.0.dylib \
  "$DEST_LIBWHISPER"

# libggml depends on libggml-base.
install_name_tool \
  -id   @rpath/libggml.0.dylib \
  -change /opt/homebrew/opt/ggml/lib/libggml-base.0.dylib @rpath/libggml-base.0.dylib \
  "$DEST_LIBGGML"

install_name_tool -id @rpath/libggml-base.0.dylib "$DEST_LIBGGML_BASE"

# Re-sign ad-hoc — modifying a Mach-O invalidates the signature.
echo "re-signing…"
codesign --force --sign - "$DEST_LIBWHISPER"
codesign --force --sign - "$DEST_LIBGGML"
codesign --force --sign - "$DEST_LIBGGML_BASE"
codesign --force --sign - "$DEST_BIN"

# Sanity check.
echo "verifying…"
otool -L "$DEST_BIN" | grep -E "libwhisper|libggml" | sed 's/^/  /'
echo "ok: bundled binary + 3 dylibs ($(du -sh "$DEST_DIR" | cut -f1))"
echo "next:  go build ./...   (the binaries are now embedded)"
