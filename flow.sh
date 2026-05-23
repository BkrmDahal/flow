#!/bin/bash
set -e

export PATH="$HOME/go/bin:$PATH"

APP_NAME="Flow"
APP_BUNDLE="build/bin/Flow.app"
DMG_NAME="Flow-Installer"
DMG_DIR="build/dmg"
DMG_OUTPUT="build/${DMG_NAME}.dmg"
VERSION="0.5.0"

usage() {
    echo "Usage: ./flow.sh <command>"
    echo ""
    echo "Commands:"
    echo "  dev          Start dev mode (hot-reload frontend + live Go recompilation)"
    echo "  build        Build production Mac app (Apple Silicon)"
    echo "  universal    Build universal binary (Intel + Apple Silicon)"
    echo "  dmg          Build app and create DMG installer"
    echo "  sign         Sign, notarize, and staple the app for public distribution"
    echo "  open         Launch the built .app"
    echo "  doctor       Check if your system is ready for Wails development"
    echo "  dequarantine Remove Gatekeeper quarantine flag from built/installed app"
    echo ""
}

case "${1:-}" in
    dev)
        echo "Starting Flow in dev mode..."
        wails dev
        ;;
    build)
        echo "Building Flow.app (darwin/arm64)..."
        wails build -platform darwin/arm64
        echo ""
        echo "Done -> ${APP_BUNDLE}"
        ;;
    universal)
        echo "Building Flow.app (universal binary)..."
        wails build -platform darwin/universal
        echo ""
        echo "Done -> ${APP_BUNDLE}"
        ;;
    open)
        open "${APP_BUNDLE}"
        ;;
    dmg)
        PLATFORM="${2:-darwin/arm64}"
        echo "==> Building Flow.app (${PLATFORM})..."
        wails build -platform "${PLATFORM}"
        echo ""

        echo "==> Creating DMG installer..."

        rm -f "${PWD}"/build/*.dmg
        rm -rf "${DMG_DIR}"
        mkdir -p "${DMG_DIR}"

        cp -R "${APP_BUNDLE}" "${DMG_DIR}/${APP_NAME}.app"
        ln -s /Applications "${DMG_DIR}/Applications"

        APP_SIZE_KB=$(du -sk "${DMG_DIR}/${APP_NAME}.app" | awk '{print $1}')
        DMG_SIZE_KB=$(( APP_SIZE_KB + 20480 ))

        TMP_DMG="build/${DMG_NAME}-tmp.dmg"
        hdiutil create \
            -srcfolder "${DMG_DIR}" \
            -volname "${APP_NAME}" \
            -fs HFS+ \
            -fsargs "-c c=64,a=16,e=16" \
            -format UDRW \
            -size "${DMG_SIZE_KB}k" \
            "${TMP_DMG}"

        MOUNT_DIR=$(hdiutil attach -readwrite -noverify "${TMP_DMG}" | \
            grep -E '^\S+\s+Apple_HFS' | awk '{print $3}')

        if [ -z "${MOUNT_DIR}" ]; then
            echo "Error: Failed to mount DMG"
            exit 1
        fi

        echo "==> Configuring DMG window layout..."
        osascript <<EOF
tell application "Finder"
    tell disk "${APP_NAME}"
        open
        set current view of container window to icon view
        set toolbar visible of container window to false
        set statusbar visible of container window to false
        set bounds of container window to {100, 100, 640, 440}
        set theViewOptions to icon view options of container window
        set arrangement of theViewOptions to not arranged
        set icon size of theViewOptions to 100
        set position of item "${APP_NAME}.app" of container window to {140, 160}
        set position of item "Applications" of container window to {400, 160}
        close
        open
        update without registering applications
        delay 2
        close
    end tell
end tell
EOF

        sync
        hdiutil detach "${MOUNT_DIR}" -quiet

        hdiutil convert "${TMP_DMG}" \
            -format UDZO \
            -imagekey zlib-level=9 \
            -o "${DMG_OUTPUT}"

        rm -f "${TMP_DMG}"
        rm -rf "${DMG_DIR}"

        DMG_SIZE=$(du -h "${DMG_OUTPUT}" | awk '{print $1}')
        echo ""
        echo "==> Done! DMG created at: ${DMG_OUTPUT} (${DMG_SIZE})"
        echo "    Open it with: open ${DMG_OUTPUT}"
        ;;
    doctor)
        wails doctor
        ;;
    dequarantine)
        ./dequarantine.sh "${2:-}"
        ;;
    sign|release)
        ./scripts/sign.sh
        ;;
    *)
        usage
        ;;
esac
