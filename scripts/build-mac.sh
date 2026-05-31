#!/usr/bin/env bash
set -euo pipefail

APP_NAME="UniviewCodeBuddyProxy"
BINARY_NAME="uniview-codebuddy-proxy"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo none)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-s -w -X uniview-codebuddy-proxy/internal/version.Version=${VERSION} -X uniview-codebuddy-proxy/internal/version.Commit=${COMMIT} -X uniview-codebuddy-proxy/internal/version.Date=${DATE}"

# Determine architecture
ARCH=${1:-$(uname -m)}
case "$ARCH" in
    arm64) GOARCH="arm64" ;;
    x86_64|amd64) GOARCH="amd64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "Building ${APP_NAME} for darwin/${GOARCH}..."

HELPER_NAME="codebuddy-proxy-helper"

# Build the main binary (with GUI/systray support)
CGO_ENABLED=1 GOOS=darwin GOARCH=$GOARCH \
    go build -tags gui -ldflags "$LDFLAGS" -o "${BINARY_NAME}" ./cmd/proxy

# Build the minimal helper binary (no systray/gin, only --login-item logic)
CGO_ENABLED=0 GOOS=darwin GOARCH=$GOARCH \
    go build -ldflags "-s -w" -o "${HELPER_NAME}" ./cmd/helper

# Compress binaries with UPX if available
# ⚠️ 仅压缩 helper 二进制，主二进制跳过
# macOS 内核代码签名验证在运行时检查内存中的二进制内容
# UPX 在运行时解压会修改内存映像，导致 POSIX error 153 (ENOTSUP)
# 签名验证失败，应用无法启动
if command -v upx &>/dev/null; then
    echo "Compressing helper binary with UPX..."
    upx -5 --force-macos "${HELPER_NAME}" 2>/dev/null || true
else
    echo "UPX not found, skipping compression (install with: brew install upx)"
fi

# Create .app bundle structure
APP_BUNDLE="${APP_NAME}.app"
CONTENTS="${APP_BUNDLE}/Contents"
MACOS="${CONTENTS}/MacOS"
RESOURCES="${CONTENTS}/Resources"
LOGIN_ITEMS="${CONTENTS}/Library/LoginItems"
HELPER_APP="${LOGIN_ITEMS}/${APP_NAME} Helper.app"
HELPER_MACOS="${HELPER_APP}/Contents/MacOS"

rm -rf "$APP_BUNDLE"

mkdir -p "$MACOS"
mkdir -p "$RESOURCES"
mkdir -p "$HELPER_MACOS"

# Copy main binary
cp "${BINARY_NAME}" "${MACOS}/${BINARY_NAME}"
chmod +x "${MACOS}/${BINARY_NAME}"

# Copy helper binary (minimal binary, only --login-item logic)
cp "${HELPER_NAME}" "${HELPER_MACOS}/${BINARY_NAME}"
chmod +x "${HELPER_MACOS}/${BINARY_NAME}"

# Create Info.plist for main app
cat > "${CONTENTS}/Info.plist" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>${BINARY_NAME}</string>
    <key>CFBundleIdentifier</key>
    <string>com.codebuddy.proxy</string>
    <key>CFBundleName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleDisplayName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>LSUIElement</key>
    <true/>
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
</dict>
</plist>
PLIST

# Create Info.plist for helper app
cat > "${HELPER_APP}/Contents/Info.plist" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>${BINARY_NAME}</string>
    <key>CFBundleIdentifier</key>
    <string>com.codebuddy.proxy.helper</string>
    <key>CFBundleName</key>
    <string>${APP_NAME} Helper</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>LSUIElement</key>
    <true/>
    <key>LSBackgroundOnly</key>
    <true/>
</dict>
</plist>
PLIST

# Generate optimized icon from source PNG (smaller than pre-built icns)
if [ -f "assets/icons/icon.png" ]; then
    ICONSET_DIR=$(mktemp -d)/icon.iconset
    mkdir -p "$ICONSET_DIR"
    for size in 16 32 64 128 256 512; do
        sips -z $size $size "assets/icons/icon.png" --out "${ICONSET_DIR}/icon_${size}x${size}.png" -s format png >/dev/null 2>&1
    done
    for size in 16 32 64 128 256; do
        sips -z $((size*2)) $((size*2)) "assets/icons/icon.png" --out "${ICONSET_DIR}/icon_${size}x${size}@2x.png" -s format png >/dev/null 2>&1
    done
    iconutil -c icns "$ICONSET_DIR" -o "${RESOURCES}/AppIcon.icns" 2>/dev/null
    rm -rf "$(dirname "$ICONSET_DIR")"
elif [ -f "assets/icons/icon.icns" ]; then
    cp assets/icons/icon.icns "${RESOURCES}/AppIcon.icns"
fi

# Ad-hoc sign the helper app (nested bundle) before signing the main bundle
codesign --force --sign - "${HELPER_APP}"

# Ad-hoc sign the main app bundle (seals resources, binds Info.plist)
codesign --force --sign - "${APP_BUNDLE}"

echo "Built ${APP_BUNDLE} successfully"
echo "Size: $(du -sh "${APP_BUNDLE}" | awk '{print $1}')"
