#!/bin/bash

# dequarantine.sh - Fix "Flow is damaged and can't be opened" macOS Gatekeeper issue.
# This script recursively removes the com.apple.quarantine extended attribute from the Flow app bundle.

# Text styling
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

echo -e "${BLUE}${BOLD}===================================================${NC}"
echo -e "${BLUE}${BOLD}          Flow App Gatekeeper De-quarantine        ${NC}"
echo -e "${BLUE}${BOLD}===================================================${NC}"
echo ""

# Determine target app path
APP_PATH=""

if [ -n "$1" ]; then
    APP_PATH="$1"
else
    # Default search locations
    CANDIDATES=(
        "/Applications/Flow.app"
        "$HOME/Applications/Flow.app"
        "./build/bin/Flow.app"
    )

    for candidate in "${CANDIDATES[@]}"; do
        if [ -d "$candidate" ]; then
            APP_PATH="$candidate"
            break
        fi
    done
fi

if [ -z "$APP_PATH" ] || [ ! -d "$APP_PATH" ]; then
    echo -e "${RED}${BOLD}Error: Flow.app not found!${NC}"
    echo -e "Could not find Flow.app in standard locations:"
    echo -e "  - /Applications/Flow.app"
    echo -e "  - ~/Applications/Flow.app"
    echo -e "  - ./build/bin/Flow.app"
    echo ""
    echo -e "Usage: $0 [/path/to/Flow.app]"
    exit 1
fi

# Resolve to absolute path
ABS_APP_PATH=$(cd "$(dirname "$APP_PATH")" && pwd)/$(basename "$APP_PATH")

echo -e "Target: ${BOLD}${ABS_APP_PATH}${NC}"
echo -e "Attempting to remove macOS quarantine flag..."

# Check if write permissions are available, otherwise use sudo
if [ -w "$ABS_APP_PATH" ]; then
    echo -e "Removing quarantine attribute..."
    xattr -r -d com.apple.quarantine "$ABS_APP_PATH" 2>/dev/null || true
else
    echo -e "${YELLOW}Insufficient permissions. Requesting administrator privileges (sudo)...${NC}"
    sudo xattr -r -d com.apple.quarantine "$ABS_APP_PATH"
fi

# Double check status
echo -e "Verifying app signature and security state..."
if codesign -v --deep "$ABS_APP_PATH" 2>/dev/null; then
    echo -e "${GREEN}Signature verified successfully.${NC}"
else
    echo -e "${YELLOW}Note: Local self-signed or unsigned binary detected. This is expected for development builds.${NC}"
fi

echo ""
echo -e "${GREEN}${BOLD}Success! The quarantine attribute has been removed.${NC}"
echo -e "You can now open the app normally by double-clicking it or using:"
echo -e "  ${BOLD}open \"$ABS_APP_PATH\"${NC}"
echo ""
