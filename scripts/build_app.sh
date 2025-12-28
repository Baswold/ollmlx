#!/bin/bash
set -euo pipefail

# ollmlx macOS App Build Script
# Builds the menu bar app and packages it with the CLI tools

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
APP_DIR="$ROOT_DIR/app/macOS"
DIST_DIR="$ROOT_DIR/dist"
VERSION="${VERSION:-$(git describe --tags --first-parent --abbrev=7 --long --dirty --always 2>/dev/null || echo "1.0.0")}"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

log() { echo -e "${BLUE}[build]${NC} $1"; }
ok() { echo -e "${GREEN}[ok]${NC} $1"; }
err() { echo -e "${RED}[error]${NC} $1" >&2; exit 1; }

# Check we're on macOS
if [[ "$(uname)" != "Darwin" ]]; then
    err "This script must be run on macOS"
fi

# Check for Swift
if ! command -v swiftc &> /dev/null; then
    err "Swift compiler not found. Please install Xcode or Xcode Command Line Tools."
fi

log "Building ollmlx v${VERSION}"

# Create dist directory
mkdir -p "$DIST_DIR"

# Build Go binaries
log "Building Go binaries..."

# Build for both architectures
for ARCH in arm64 amd64; do
    log "  Building for darwin-$ARCH..."
    mkdir -p "$DIST_DIR/darwin-$ARCH"

    GOARCH="$ARCH"
    if [[ "$ARCH" == "amd64" ]]; then
        GOARCH="amd64"
    fi

    CGO_ENABLED=1 GOOS=darwin GOARCH="$GOARCH" go build \
        -ldflags="-s -w -X github.com/ollama/ollama/version.Version=${VERSION}" \
        -o "$DIST_DIR/darwin-$ARCH/ollmlx" \
        "$ROOT_DIR"

    ok "  Built darwin-$ARCH/ollmlx"
done

# Create universal binary
log "Creating universal binary..."
mkdir -p "$DIST_DIR/darwin"
lipo -create \
    "$DIST_DIR/darwin-arm64/ollmlx" \
    "$DIST_DIR/darwin-amd64/ollmlx" \
    -output "$DIST_DIR/darwin/ollmlx"
chmod +x "$DIST_DIR/darwin/ollmlx"
ok "Universal binary created"

# Build Swift app
log "Building Swift menu bar app..."
cd "$APP_DIR"

# Build for both architectures
for ARCH in arm64 x86_64; do
    log "  Compiling for $ARCH..."
    swiftc \
        -O \
        -target "${ARCH}-apple-macosx14.0" \
        -o "$DIST_DIR/OllmlxApp-$ARCH" \
        Sources/main.swift
done

# Create universal app binary
log "Creating universal app binary..."
lipo -create \
    "$DIST_DIR/OllmlxApp-arm64" \
    "$DIST_DIR/OllmlxApp-x86_64" \
    -output "$DIST_DIR/OllmlxApp"
chmod +x "$DIST_DIR/OllmlxApp"
ok "Universal app binary created"

# Create app bundle
log "Creating app bundle..."
APP_BUNDLE="$DIST_DIR/ollmlx.app"
rm -rf "$APP_BUNDLE"
mkdir -p "$APP_BUNDLE/Contents/MacOS"
mkdir -p "$APP_BUNDLE/Contents/Resources"

# Copy app binary
cp "$DIST_DIR/OllmlxApp" "$APP_BUNDLE/Contents/MacOS/OllmlxApp"

# Copy Info.plist and update version
cp "$APP_DIR/OllmlxApp.app/Contents/Info.plist" "$APP_BUNDLE/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $VERSION" "$APP_BUNDLE/Contents/Info.plist" 2>/dev/null || true
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $VERSION" "$APP_BUNDLE/Contents/Info.plist" 2>/dev/null || true

# Copy CLI binary to Resources
cp "$DIST_DIR/darwin/ollmlx" "$APP_BUNDLE/Contents/Resources/ollmlx"
chmod +x "$APP_BUNDLE/Contents/Resources/ollmlx"

# Copy MLX backend
if [[ -d "$ROOT_DIR/mlx_backend" ]]; then
    cp -r "$ROOT_DIR/mlx_backend" "$APP_BUNDLE/Contents/Resources/mlx_backend"
fi

# Create simple icon (placeholder)
log "Creating app icon..."
cat > "$DIST_DIR/create_icon.py" << 'PYEOF'
#!/usr/bin/env python3
import os
import sys

# Create a simple icns file with a placeholder
# In production, you'd use a real icon

iconset_dir = sys.argv[1] if len(sys.argv) > 1 else "AppIcon.iconset"
os.makedirs(iconset_dir, exist_ok=True)

# Create a minimal PNG (1x1 blue pixel as placeholder)
# Real icons should be created with proper design tools
sizes = [16, 32, 64, 128, 256, 512, 1024]

for size in sizes:
    filename = f"icon_{size}x{size}.png"
    filepath = os.path.join(iconset_dir, filename)

    # Create minimal valid PNG
    import struct
    import zlib

    def create_png(width, height, color=(66, 133, 244)):
        """Create a simple solid color PNG"""
        def png_chunk(chunk_type, data):
            chunk_len = struct.pack('>I', len(data))
            chunk_crc = struct.pack('>I', zlib.crc32(chunk_type + data) & 0xffffffff)
            return chunk_len + chunk_type + data + chunk_crc

        # PNG signature
        signature = b'\x89PNG\r\n\x1a\n'

        # IHDR chunk
        ihdr_data = struct.pack('>IIBBBBB', width, height, 8, 2, 0, 0, 0)
        ihdr = png_chunk(b'IHDR', ihdr_data)

        # IDAT chunk (image data)
        raw_data = b''
        for y in range(height):
            raw_data += b'\x00'  # filter type
            for x in range(width):
                raw_data += bytes(color)

        compressed = zlib.compress(raw_data, 9)
        idat = png_chunk(b'IDAT', compressed)

        # IEND chunk
        iend = png_chunk(b'IEND', b'')

        return signature + ihdr + idat + iend

    # Blue color for ollmlx branding
    png_data = create_png(size, size, (66, 133, 244))
    with open(filepath, 'wb') as f:
        f.write(png_data)

print(f"Created iconset at {iconset_dir}")
PYEOF

python3 "$DIST_DIR/create_icon.py" "$DIST_DIR/AppIcon.iconset"
iconutil -c icns -o "$APP_BUNDLE/Contents/Resources/AppIcon.icns" "$DIST_DIR/AppIcon.iconset" 2>/dev/null || true

# Cleanup temp files
rm -rf "$DIST_DIR/AppIcon.iconset" "$DIST_DIR/create_icon.py"
rm -f "$DIST_DIR/OllmlxApp-arm64" "$DIST_DIR/OllmlxApp-x86_64" "$DIST_DIR/OllmlxApp"

ok "App bundle created at $APP_BUNDLE"

# Touch to update modification time
touch "$APP_BUNDLE"

# Code sign if identity is available
if [[ -n "${APPLE_IDENTITY:-}" ]]; then
    log "Code signing app bundle..."
    codesign -f --timestamp -s "$APPLE_IDENTITY" --identifier com.ollmlx.app --options=runtime "$APP_BUNDLE"
    ok "Code signed"
else
    log "Skipping code signing (no APPLE_IDENTITY set)"
fi

echo ""
echo "============================================"
echo -e "${GREEN}Build complete!${NC}"
echo ""
echo "  App bundle: $APP_BUNDLE"
echo "  CLI binary: $DIST_DIR/darwin/ollmlx"
echo ""
echo "To create a DMG installer, run:"
echo "  ./scripts/package_dmg.sh"
echo "============================================"
