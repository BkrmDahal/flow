#!/bin/bash
# scripts/sign.sh - Automated macOS code signing, packaging, and notarization.
# Made for Flow - Wails Application.

set -e

# Text styling
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

APP_NAME="Flow"
APP_BUNDLE="build/bin/Flow.app"
DMG_OUTPUT="build/Flow-Installer.dmg"
ENTITLEMENTS="build/darwin/entitlements.plist"

echo -e "${BLUE}${BOLD}===================================================${NC}"
echo -e "${BLUE}${BOLD}        macOS Signing & Notarization Utility       ${NC}"
echo -e "${BLUE}${BOLD}===================================================${NC}"
echo ""

# Check if entitlements file exists
if [ ! -f "$ENTITLEMENTS" ]; then
    echo -e "${RED}Error: Entitlements file not found at: ${ENTITLEMENTS}${NC}"
    exit 1
fi

# Setup default variables or load from environment
DEVELOPER_ID="${DEVELOPER_ID:-}"
NOTARY_PROFILE="${NOTARY_PROFILE:-flow-notary-profile}"

# 1. Helper to find available Developer ID certificates
find_certificates() {
    echo -e "${BLUE}Scanning Keychain for Developer ID Application certificates...${NC}"
    security find-certificate -a -c "Developer ID Application" | grep "attributes" || true
}

# Prompt for Developer ID if not set
if [ -z "$DEVELOPER_ID" ]; then
    echo -e "${YELLOW}No DEVELOPER_ID environment variable set.${NC}"
    echo -e "Available certificates on your Mac:"
    echo -e "---------------------------------------------------"
    security find-identity -v -p codesigning | grep "Developer ID Application" || echo "No Developer ID Application certificates found."
    echo -e "---------------------------------------------------"
    echo -e "Please enter your Developer ID Application certificate name"
    echo -e "Example: ${BOLD}Developer ID Application: John Doe (A1B2C3D4E5)${NC}"
    read -p "Certificate Name: " DEVELOPER_ID
fi

if [ -z "$DEVELOPER_ID" ]; then
    echo -e "${RED}Error: Developer ID Application certificate is required to sign the app.${NC}"
    exit 1
fi

# Resolve Team ID to full Certificate Name if a 10-character Team ID is passed
if [[ "$DEVELOPER_ID" =~ ^[A-Z0-9]{10}$ ]]; then
    echo -e "${BLUE}DEVELOPER_ID looks like a Team ID (${DEVELOPER_ID}). Searching Keychain...${NC}"
    MATCHING_CERT=$(security find-identity -v -p codesigning | grep "Developer ID Application" | grep "(${DEVELOPER_ID})" | head -n 1 | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -n "$MATCHING_CERT" ]; then
        echo -e "${GREEN}Resolved to certificate: ${BOLD}${MATCHING_CERT}${NC}"
        DEVELOPER_ID="$MATCHING_CERT"
    else
        # If no Developer ID certificate matches the Team ID, check for any Developer ID certificate
        MATCHING_ANY=$(security find-identity -v -p codesigning | grep "Developer ID Application" | head -n 1 | sed -E 's/.*"([^"]+)".*/\1/')
        if [ -n "$MATCHING_ANY" ]; then
            echo -e "${YELLOW}Warning: No 'Developer ID Application' certificate found with Team ID ${DEVELOPER_ID}.${NC}"
            echo -e "Falling back to the first available Developer ID certificate: ${BOLD}${MATCHING_ANY}${NC}"
            DEVELOPER_ID="$MATCHING_ANY"
        else
            echo -e "${RED}Error: Could not find any valid 'Developer ID Application' certificates in your Keychain matching Team ID '${DEVELOPER_ID}'.${NC}"
            echo -e "Make sure you generated a 'Developer ID Application' certificate (not 'Apple Development') on developer.apple.com and double-clicked it to install it into Keychain Access."
            exit 1
        fi
    fi
fi


# Check if Apple notary profile exists
echo -e "${BLUE}Checking Apple Notary Profile: ${BOLD}${NOTARY_PROFILE}${NC}..."
if ! xcrun notarytool history --keychain-profile "$NOTARY_PROFILE" >/dev/null 2>&1; then
    echo -e "${YELLOW}Warning: Keychain profile '${NOTARY_PROFILE}' could not be verified.${NC}"
    echo -e "If you haven't set up your notary credentials yet, run this command in a separate terminal:"
    echo -e "  ${BOLD}xcrun notarytool store-credentials \"${NOTARY_PROFILE}\" --apple-id \"<your-apple-id>\" --team-id \"<your-team-id>\" --password \"<app-specific-password>\"${NC}"
    echo ""
    read -p "Do you want to continue anyway? (y/N): " CONTINUE
    if [[ ! "$CONTINUE" =~ ^[Yy]$ ]]; then
        echo -e "${RED}Aborted. Please configure your Apple Developer credentials first.${NC}"
        exit 1
    fi
fi

# 2. Build the app bundle
echo -e "\n${BLUE}==> [1/6] Building production application (darwin/universal)...${NC}"
wails build -platform darwin/universal -clean

# 3. Clean up quarantine tags from build files before signing
echo -e "\n${BLUE}==> [2/6] Cleaning up quarantine and extended attributes...${NC}"
xattr -cr "$APP_BUNDLE"

# 4. Recursively sign the app bundle
echo -e "\n${BLUE}==> [3/6] Signing App Bundle with Hardened Runtime...${NC}"

# Sign nested frameworks, dylibs, or helpers if any exist
find "$APP_BUNDLE" -depth \( -name "*.dylib" -o -name "*.so" -o -name "*.framework" -o -name "whisper-cli" -o -name "llama-server" \) | while read -r nested_file; do
    if [ -f "$nested_file" ] || [ -d "$nested_file" ]; then
        echo "Signing nested binary: $(basename "$nested_file")"
        codesign --force --timestamp --options=runtime --sign "$DEVELOPER_ID" "$nested_file"
    fi
done

# Sign the main executable and bundle
echo "Signing app bundle: ${APP_BUNDLE}"
codesign --force --timestamp --options=runtime --entitlements "$ENTITLEMENTS" --sign "$DEVELOPER_ID" "$APP_BUNDLE"

# Verify the app signature
echo "Verifying app signature..."
codesign -vvv --deep --strict "$APP_BUNDLE"

# 5. Package into DMG
echo -e "\n${BLUE}==> [4/6] Creating DMG Installer...${NC}"
./flow.sh dmg darwin/universal

if [ ! -f "$DMG_OUTPUT" ]; then
    echo -e "${RED}Error: DMG Installer creation failed. Make sure ./flow.sh dmg succeeded.${NC}"
    exit 1
fi

# Sign the DMG file
echo "Signing DMG Installer: ${DMG_OUTPUT}"
codesign --force --timestamp --sign "$DEVELOPER_ID" "$DMG_OUTPUT"

# Verify the DMG signature
echo "Verifying DMG signature..."
codesign -vvv "$DMG_OUTPUT"

# 6. Notarize the DMG
echo -e "\n${BLUE}==> [5/6] Submitting DMG to Apple Notary Service (this may take a few minutes)...${NC}"
echo "Uploading ${DMG_OUTPUT} using keychain profile '${NOTARY_PROFILE}'..."

xcrun notarytool submit "$DMG_OUTPUT" --keychain-profile "$NOTARY_PROFILE" --wait

# 7. Staple the Notarization Ticket
echo -e "\n${BLUE}==> [6/6] Stapling Notarization Ticket...${NC}"
echo "Stapling ticket to DMG..."
xcrun stapler staple "$DMG_OUTPUT"

echo "Stapling ticket to App Bundle (for offline validation inside DMG)..."
xcrun stapler staple "$APP_BUNDLE"

echo -e "\n${GREEN}${BOLD}🎉 SUCCESS! Your application is fully Signed, Notarized, and Stapled!${NC}"
echo -e "Users can now download and install: ${BOLD}${DMG_OUTPUT}${NC}"
echo -e "They will not get any Gatekeeper warning dialogs or quarantine blocks."
echo ""
