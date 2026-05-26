#!/bin/bash
set -e

export PATH="$HOME/go/bin:$PATH"

APP_NAME="Flow"
APP_BUNDLE="build/bin/Flow.app"
DMG_NAME="Flow-Installer"
DMG_DIR="build/dmg"
DMG_OUTPUT="build/${DMG_NAME}.dmg"
VERSION="0.6.9"

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
        PLATFORM="darwin/arm64"
        SKIP_BUILD=false
        
        # Check if --skip-build is in the arguments
        for arg in "$@"; do
            if [ "$arg" = "--skip-build" ]; then
                SKIP_BUILD=true
            fi
        done

        # If the second argument is not --skip-build and is provided, use it as platform
        if [ "${2:-}" != "--skip-build" ] && [ -n "${2:-}" ]; then
            PLATFORM="$2"
        fi

        if [ "$SKIP_BUILD" = false ]; then
            echo "==> Building Flow.app (${PLATFORM})...."
            wails build -platform "${PLATFORM}"
            echo ""
        else
            echo "==> Skipping build, packaging existing Flow.app bundle..."
        fi

        echo "==> Creating DMG installer..."

        # 1. Proactively clean up any stale mounts of the same app volume to avoid conflict or "Resource temporarily unavailable"
        echo "Cleaning up any existing DMG mounts..."
        hdiutil info | grep -E "/Volumes/${APP_NAME}( [0-9]+)?" | awk '{print $1}' | while read -r dev; do
            if [ -n "$dev" ]; then
                echo "Detaching stale mount: $dev"
                hdiutil detach "$dev" -force 2>/dev/null || true
            fi
        done

        rm -f "${PWD}"/build/*.dmg
        rm -rf "${DMG_DIR}"
        mkdir -p "${DMG_DIR}"

        cp -R "${APP_BUNDLE}" "${DMG_DIR}/${APP_NAME}.app"
        ln -s /Applications "${DMG_DIR}/Applications"

        APP_SIZE_KB=$(du -sk "${DMG_DIR}/${APP_NAME}.app" | awk '{print $1}')
        DMG_SIZE_KB=$(( APP_SIZE_KB + 20480 ))

        TMP_DMG="build/${DMG_NAME}-tmp.dmg"
        CREATE_SUCCESS=false
        for i in {1..5}; do
            if hdiutil create \
                -srcfolder "${DMG_DIR}" \
                -volname "${APP_NAME}" \
                -fs HFS+ \
                -fsargs "-c c=64,a=16,e=16" \
                -format UDRW \
                -size "${DMG_SIZE_KB}k" \
                "${TMP_DMG}"; then
                CREATE_SUCCESS=true
                break
            fi
            echo "hdiutil create failed or resource is busy, retrying in 3 seconds... ($i/5)"
            sleep 3
        done

        if [ "$CREATE_SUCCESS" = false ]; then
            echo "Error: Failed to create temporary DMG after 5 attempts."
            exit 1
        fi


        # 2. Extract full mount path preserving spaces (using sed instead of awk to avoid truncating at spaces)
        MOUNT_DIR=$(hdiutil attach -readwrite -noverify "${TMP_DMG}" | \
            grep -E '^\S+\s+Apple_HFS' | sed 's/.*Apple_HFS[[:space:]]*//')

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
        
        # 3. Detach DMG with a retry loop to give Finder and system services time to release resources
        echo "==> Detaching DMG mount..."
        DETACH_SUCCESS=false
        for i in {1..5}; do
            if hdiutil detach "${MOUNT_DIR}" -quiet 2>/dev/null; then
                DETACH_SUCCESS=true
                break
            fi
            echo "Mount is busy, retrying in 2 seconds... ($i/5)"
            sleep 2
        done

        if [ "$DETACH_SUCCESS" = false ]; then
            echo "Warning: Mount is still busy, forcing detach..."
            hdiutil detach "${MOUNT_DIR}" -force -quiet || true
            sleep 1
        fi

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
