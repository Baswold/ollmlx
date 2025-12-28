#!/bin/bash
set -euo pipefail

# ollmlx DMG Packaging Script
# Creates a professional DMG installer for macOS

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
DIST_DIR="$ROOT_DIR/dist"
VERSION="${VERSION:-$(git describe --tags --first-parent --abbrev=7 --long --dirty --always 2>/dev/null || echo "1.0.0")}"
DMG_NAME="ollmlx-${VERSION}.dmg"
VOL_NAME="${VOL_NAME:-ollmlx}"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

log() { echo -e "${BLUE}[dmg]${NC} $1"; }
ok() { echo -e "${GREEN}[ok]${NC} $1"; }
err() { echo -e "${RED}[error]${NC} $1" >&2; exit 1; }

# Check we're on macOS
if [[ "$(uname)" != "Darwin" ]]; then
    err "This script must be run on macOS"
fi

# Check if app bundle exists
APP_BUNDLE="$DIST_DIR/ollmlx.app"
if [[ ! -d "$APP_BUNDLE" ]]; then
    log "App bundle not found. Building first..."
    "$SCRIPT_DIR/build_app.sh"
fi

log "Creating DMG installer v${VERSION}"

# Create staging directory
STAGING_DIR=$(mktemp -d)
trap "rm -rf $STAGING_DIR" EXIT

log "Preparing staging directory..."

# Copy app bundle
cp -R "$APP_BUNDLE" "$STAGING_DIR/"

# Create Applications symlink
ln -s /Applications "$STAGING_DIR/Applications"

# Create a simple README
cat > "$STAGING_DIR/README.txt" << 'EOF'
ollmlx - Apple Silicon Optimized LLM Inference
===============================================

Installation:
1. Drag ollmlx.app to your Applications folder
2. Launch ollmlx from Applications
3. The menu bar icon will appear (brain icon)

Quick Start:
- The server starts automatically when you launch the app
- Click the menu bar icon to access controls
- Use any Ollama-compatible client to connect

CLI Usage:
The ollmlx CLI is bundled inside the app. To use it:

  # Option 1: Use from app bundle
  /Applications/ollmlx.app/Contents/Resources/ollmlx --help

  # Option 2: Create a symlink
  sudo ln -sf /Applications/ollmlx.app/Contents/Resources/ollmlx /usr/local/bin/ollmlx

Commands:
  ollmlx serve              Start the server
  ollmlx pull MODEL         Download a model
  ollmlx run MODEL          Chat with a model
  ollmlx list               List installed models
  ollmlx doctor             Check system status

Example:
  ollmlx pull mlx-community/gemma-2-2b-it-4bit
  ollmlx run mlx-community/gemma-2-2b-it-4bit

Documentation: https://github.com/ollama/ollama
EOF

# Create background image for DMG
log "Creating DMG background..."
BACKGROUND_DIR="$STAGING_DIR/.background"
mkdir -p "$BACKGROUND_DIR"

python3 << 'PYEOF'
import struct
import zlib
import sys

def create_gradient_png(width, height, output_path):
    """Create a nice gradient background PNG"""
    def png_chunk(chunk_type, data):
        chunk_len = struct.pack('>I', len(data))
        chunk_crc = struct.pack('>I', zlib.crc32(chunk_type + data) & 0xffffffff)
        return chunk_len + chunk_type + data + chunk_crc

    signature = b'\x89PNG\r\n\x1a\n'

    # IHDR
    ihdr_data = struct.pack('>IIBBBBB', width, height, 8, 2, 0, 0, 0)
    ihdr = png_chunk(b'IHDR', ihdr_data)

    # Generate gradient image data
    raw_data = b''
    for y in range(height):
        raw_data += b'\x00'  # filter
        progress = y / height
        for x in range(width):
            # Dark blue to lighter blue gradient
            r = int(20 + progress * 30)
            g = int(30 + progress * 50)
            b = int(60 + progress * 80)
            raw_data += bytes([r, g, b])

    compressed = zlib.compress(raw_data, 9)
    idat = png_chunk(b'IDAT', compressed)
    iend = png_chunk(b'IEND', b'')

    with open(output_path, 'wb') as f:
        f.write(signature + ihdr + idat + iend)

import os
staging = os.environ.get('STAGING_DIR', '/tmp')
create_gradient_png(800, 400, f"{staging}/.background/background.png")
print("Background created")
PYEOF

export STAGING_DIR
python3 -c "
import struct, zlib, os
def create_png(w, h, path):
    def chunk(t, d):
        return struct.pack('>I', len(d)) + t + d + struct.pack('>I', zlib.crc32(t + d) & 0xffffffff)
    sig = b'\x89PNG\r\n\x1a\n'
    ihdr = chunk(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 2, 0, 0, 0))
    raw = b''
    for y in range(h):
        raw += b'\x00'
        p = y / h
        for x in range(w):
            raw += bytes([int(20+p*30), int(30+p*50), int(60+p*80)])
    idat = chunk(b'IDAT', zlib.compress(raw, 9))
    iend = chunk(b'IEND', b'')
    with open(path, 'wb') as f:
        f.write(sig + ihdr + idat + iend)

create_png(800, 400, '$STAGING_DIR/.background/background.png')
"
ok "Background created"

# Remove any existing DMG
rm -f "$DIST_DIR/$DMG_NAME"

log "Creating DMG..."

# Use create-dmg if available, otherwise use hdiutil
if [[ -x "$SCRIPT_DIR/create-dmg.sh" ]]; then
    "$SCRIPT_DIR/create-dmg.sh" \
        --volname "$VOL_NAME" \
        --window-pos 200 120 \
        --window-size 800 400 \
        --icon-size 100 \
        --icon "ollmlx.app" 200 190 \
        --hide-extension "ollmlx.app" \
        --app-drop-link 600 190 \
        --background "$STAGING_DIR/.background/background.png" \
        --text-size 14 \
        --skip-jenkins \
        "$DIST_DIR/$DMG_NAME" \
        "$STAGING_DIR"
else
    # Fallback to basic hdiutil
    log "Using basic hdiutil (create-dmg.sh not found)..."

    # Create temp DMG
    TEMP_DMG=$(mktemp -u).dmg
    hdiutil create -srcfolder "$STAGING_DIR" -volname "$VOL_NAME" -fs HFS+ -fsargs "-c c=64,a=16,e=16" -format UDRW "$TEMP_DMG"

    # Mount and customize
    DEV_NAME=$(hdiutil attach -readwrite -noverify -noautoopen "$TEMP_DMG" | grep -E '^/dev/' | head -1 | awk '{print $1}')
    MOUNT_DIR=$(hdiutil info | grep -E "$DEV_NAME" | awk '{print $3}' | head -1)

    # Set window properties using AppleScript
    osascript << APPLESCRIPT
tell application "Finder"
    tell disk "$VOL_NAME"
        open
        set current view of container window to icon view
        set toolbar visible of container window to false
        set statusbar visible of container window to false
        set bounds of container window to {200, 120, 1000, 520}
        set viewOptions to icon view options of container window
        set arrangement of viewOptions to not arranged
        set icon size of viewOptions to 100
        set position of item "ollmlx.app" of container window to {200, 190}
        set position of item "Applications" of container window to {600, 190}
        set position of item "README.txt" of container window to {400, 320}
        close
        open
        update without registering applications
        delay 2
    end tell
end tell
APPLESCRIPT

    # Finalize
    chmod -Rf go-w "$MOUNT_DIR" 2>/dev/null || true
    hdiutil detach "$DEV_NAME"
    hdiutil convert "$TEMP_DMG" -format UDZO -imagekey zlib-level=9 -o "$DIST_DIR/$DMG_NAME"
    rm -f "$TEMP_DMG"
fi

ok "DMG created at $DIST_DIR/$DMG_NAME"

# Code sign DMG if identity is available
if [[ -n "${APPLE_IDENTITY:-}" ]]; then
    log "Code signing DMG..."
    codesign -f --timestamp -s "$APPLE_IDENTITY" --identifier com.ollmlx.dmg "$DIST_DIR/$DMG_NAME"

    # Notarize if credentials are available
    if [[ -n "${APPLE_ID:-}" && -n "${APPLE_PASSWORD:-}" && -n "${APPLE_TEAM_ID:-}" ]]; then
        log "Submitting for notarization..."
        xcrun notarytool submit "$DIST_DIR/$DMG_NAME" \
            --wait --timeout 10m \
            --apple-id "$APPLE_ID" \
            --password "$APPLE_PASSWORD" \
            --team-id "$APPLE_TEAM_ID"

        log "Stapling notarization ticket..."
        xcrun stapler staple "$DIST_DIR/$DMG_NAME"
        ok "Notarization complete"
    fi
fi

# Get file size
DMG_SIZE=$(du -h "$DIST_DIR/$DMG_NAME" | cut -f1)

echo ""
echo "============================================"
echo -e "${GREEN}DMG packaging complete!${NC}"
echo ""
echo "  File: $DIST_DIR/$DMG_NAME"
echo "  Size: $DMG_SIZE"
echo "  Version: $VERSION"
echo ""
echo "To install:"
echo "  1. Open the DMG"
echo "  2. Drag ollmlx.app to Applications"
echo "  3. Launch from Applications"
echo "============================================"
