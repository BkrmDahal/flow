#!/bin/bash
# scripts/release.sh - Build, sign, notarize, and publish to GitHub Releases

set -e

# Text styling
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

VERSION="0.6.0"
DMG_OUTPUT="build/Flow-Installer.dmg"

echo -e "${BLUE}${BOLD}===================================================${NC}"
echo -e "${BLUE}${BOLD}        Flow GitHub Automated Release Utility      ${NC}"
echo -e "${BLUE}${BOLD}===================================================${NC}"
echo ""

# 1. Verify GitHub CLI is installed and authenticated
if ! command -v gh &> /dev/null; then
    echo -e "${RED}Error: GitHub CLI (gh) is not installed. Install it via Homebrew:${NC}"
    echo -e "  brew install gh"
    exit 1
fi

if ! gh auth status &> /dev/null; then
    echo -e "${RED}Error: GitHub CLI is not authenticated. Please run:${NC}"
    echo -e "  gh auth login"
    exit 1
fi

# 2. Run the signing and notarization workflow
echo -e "${BLUE}Starting build, signing, and notarization...${NC}"
./flow.sh sign

if [ ! -f "$DMG_OUTPUT" ]; then
    echo -e "${RED}Error: Signed DMG not found at ${DMG_OUTPUT}${NC}"
    exit 1
fi

# 3. Create Git Tag and push
echo -e "\n${BLUE}==> Creating and pushing git tag v${VERSION}...${NC}"
# Delete tag locally and on remote if it already exists to avoid conflicts
git tag -d "v${VERSION}" 2>/dev/null || true
git push origin :refs/tags/v"${VERSION}" 2>/dev/null || true

# Tag and push
git tag -a "v${VERSION}" -m "Release v${VERSION}"
CURRENT_BRANCH=$(git branch --show-current)
git push origin "$CURRENT_BRANCH" --tags

# 4. Create GitHub Release and upload DMG
echo -e "\n${BLUE}==> Creating GitHub Release v${VERSION}...${NC}"
gh release create "v${VERSION}" "$DMG_OUTPUT" \
    --title "v${VERSION}" \
    --notes "Release v${VERSION} with macOS Code Signing & Notarization." \
    --target "$CURRENT_BRANCH"

echo -e "\n${GREEN}${BOLD}🎉 SUCCESS! Version v${VERSION} has been successfully built, signed, notarized, and released to GitHub!${NC}"
echo -e "Check it out at: ${BOLD}https://github.com/BkrmDahal/flow/releases/tag/v${VERSION}${NC}"
echo ""
