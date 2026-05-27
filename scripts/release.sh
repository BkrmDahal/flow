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

VERSION="0.7.0"
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
echo -e "\n${BLUE}==> Extracting release notes from CHANGELOG.md...${NC}"
NOTES_FILE="build/release-notes-v${VERSION}.md"
mkdir -p build
python3 -c "
import re, sys
version = '${VERSION}'
with open('CHANGELOG.md', 'r') as f:
    content = f.read()
pattern = re.compile(rf'##\s+\[{re.escape(version)}\].*?(?=\n##\s+\[|\Z)', re.DOTALL)
match = pattern.search(content)
if match:
    lines = match.group(0).strip().split('\n')
    notes = '\n'.join(lines[1:]).strip()
    with open('${NOTES_FILE}', 'w') as out:
        out.write(notes)
else:
    print('Error: Release notes for version ' + version + ' not found in CHANGELOG.md')
    sys.exit(1)
"

echo -e "${BLUE}==> Creating GitHub Release v${VERSION} with detailed notes...${NC}"
gh release create "v${VERSION}" "$DMG_OUTPUT" \
    --title "v${VERSION}" \
    --notes-file "$NOTES_FILE" \
    --target "$CURRENT_BRANCH"

# Clean up temporary notes file
rm -f "$NOTES_FILE"

echo -e "\n${GREEN}${BOLD}🎉 SUCCESS! Version v${VERSION} has been successfully built, signed, notarized, and released to GitHub!${NC}"
echo -e "Check it out at: ${BOLD}https://github.com/BkrmDahal/flow/releases/tag/v${VERSION}${NC}"
echo ""
