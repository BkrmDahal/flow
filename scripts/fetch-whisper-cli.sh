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
SRC_LIBOMP="$(readlink -f /opt/homebrew/opt/libomp/lib/libomp.dylib 2>/dev/null || true)"
SRC_LIBGGML_BACKENDS_DIR="$(readlink -f /opt/homebrew/opt/ggml/libexec 2>/dev/null || true)"

for dep in "$SRC_LIBWHISPER" "$SRC_LIBGGML" "$SRC_LIBGGML_BASE" "$SRC_LIBOMP"; do
  if [[ -z "$dep" || ! -f "$dep" ]]; then
    echo "error: missing dylib dependency: $dep" >&2
    echo "       run: brew install whisper-cpp ggml libomp" >&2
    exit 1
  fi
done

if [[ -z "$SRC_LIBGGML_BACKENDS_DIR" || ! -d "$SRC_LIBGGML_BACKENDS_DIR" ]]; then
  echo "error: missing ggml backends directory: $SRC_LIBGGML_BACKENDS_DIR" >&2
  echo "       run: brew install ggml" >&2
  exit 1
fi

echo "copying $SRC_BIN → $DEST_BIN"
cp "$SRC_BIN" "$DEST_BIN"
chmod 0755 "$DEST_BIN"

DEST_LIBWHISPER="$LIB_DIR/libwhisper.1.dylib"
DEST_LIBGGML="$LIB_DIR/libggml.0.dylib"
DEST_LIBGGML_BASE="$LIB_DIR/libggml-base.0.dylib"
DEST_LIBOMP="$LIB_DIR/libomp.dylib"
BACKENDS_DIR="$DEST_DIR/backends"

mkdir -p "$BACKENDS_DIR"

echo "copying dylibs → $LIB_DIR/"
cp "$SRC_LIBWHISPER"     "$DEST_LIBWHISPER"
cp "$SRC_LIBGGML"        "$DEST_LIBGGML"
cp "$SRC_LIBGGML_BASE"   "$DEST_LIBGGML_BASE"
cp "$SRC_LIBOMP"         "$DEST_LIBOMP"
chmod 0755 "$LIB_DIR"/*.dylib

echo "copying backends → $BACKENDS_DIR/"
cp "$SRC_LIBGGML_BACKENDS_DIR"/libggml-*.so "$BACKENDS_DIR/"
chmod 0755 "$BACKENDS_DIR"/*.so

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
install_name_tool -id @rpath/libomp.dylib "$DEST_LIBOMP"

# Rewrite absolute /opt/homebrew openmp paths in backends
echo "rewriting openmp paths in backends…"
for backend_so in "$BACKENDS_DIR"/libggml-*.so; do
  install_name_tool \
    -change /opt/homebrew/opt/libomp/lib/libomp.dylib @rpath/libomp.dylib \
    "$backend_so"
done

# Re-sign ad-hoc — modifying a Mach-O invalidates the signature.
echo "re-signing…"
codesign --force --sign - "$DEST_LIBWHISPER"
codesign --force --sign - "$DEST_LIBGGML"
codesign --force --sign - "$DEST_LIBGGML_BASE"
codesign --force --sign - "$DEST_LIBOMP"
for backend_so in "$BACKENDS_DIR"/libggml-*.so; do
  codesign --force --sign - "$backend_so"
done
codesign --force --sign - "$DEST_BIN"

# Sanity check.
echo "verifying…"
otool -L "$DEST_BIN" | grep -E "libwhisper|libggml" | sed 's/^/  /'
echo "ok: bundled binary + 4 dylibs + backends ($(du -sh "$DEST_DIR" | cut -f1))"
echo "next:  go build ./...   (the binaries are now embedded)"
