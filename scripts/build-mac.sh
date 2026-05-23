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

# Build the binary
CGO_ENABLED=1 GOOS=darwin GOARCH=$GOARCH \
    go build -ldflags "$LDFLAGS" -o "${BINARY_NAME}" ./cmd/proxy

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

# Copy helper binary (same binary, --login-item flag distinguishes role)
cp "${BINARY_NAME}" "${HELPER_MACOS}/${BINARY_NAME}"

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

# Copy icon if available
if [ -f "assets/icons/icon.icns" ]; then
    cp assets/icons/icon.icns "${RESOURCES}/AppIcon.icns"
fi

echo "Built ${APP_BUNDLE} successfully"
