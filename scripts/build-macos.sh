#!/usr/bin/env bash
set -euo pipefail

# ─── Configuration ───────────────────────────────────────────────
APP_NAME="UniviewCodeBuddyProxy"
BUNDLE_ID="com.uniview.codebuddy-proxy"
DISPLAY_NAME="Uniview CodeBuddy Proxy"
VERSION="${APP_VERSION:-0.0.0-dev}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MACOS_DIR="$PROJECT_ROOT/macos/UniviewCodeBuddyProxy"
ASSETS_DIR="$PROJECT_ROOT/assets/icons"
DIST_DIR="$PROJECT_ROOT/dist"

APP_BUNDLE="$DIST_DIR/$APP_NAME.app"
DMG_NAME="$APP_NAME-macOS.dmg"
DMG_PATH="$DIST_DIR/$DMG_NAME"

# ─── Banner ──────────────────────────────────────────────────────
echo ""
echo "╔══════════════════════════════════════════════════════╗"
echo "║  Building $APP_NAME v$VERSION for macOS"
echo "╚══════════════════════════════════════════════════════╝"

# ─── 1. Build universal binary ───────────────────────────────────
echo ""
echo "📦 Step 1/6: Building arm64 binary..."
cd "$MACOS_DIR"
swift build -c release --arch arm64

echo "📦 Step 1/6: Building x86_64 binary..."
swift build -c release --arch x86_64

echo "📦 Step 1/6: Creating universal binary..."
mkdir -p .build/universal
lipo -create \
  .build/arm64-apple-macosx/release/$APP_NAME \
  .build/x86_64-apple-macosx/release/$APP_NAME \
  -output .build/universal/$APP_NAME

echo "  ✅ Universal binary:"
lipo -info .build/universal/$APP_NAME

# ─── 2. Assemble .app bundle ────────────────────────────────────
echo ""
echo "📦 Step 2/6: Assembling .app bundle..."

rm -rf "$APP_BUNDLE"
mkdir -p "$APP_BUNDLE/Contents/MacOS"
mkdir -p "$APP_BUNDLE/Contents/Resources"

# Copy executable
cp ".build/universal/$APP_NAME" "$APP_BUNDLE/Contents/MacOS/$APP_NAME"
chmod +x "$APP_BUNDLE/Contents/MacOS/$APP_NAME"

# Copy and patch Info.plist
cp "$MACOS_DIR/Info.plist" "$APP_BUNDLE/Contents/Info.plist"
sed -i '' "s/0\.0\.0/$VERSION/g" "$APP_BUNDLE/Contents/Info.plist"

# Copy icon
if [[ -f "$ASSETS_DIR/icon.icns" ]]; then
  cp "$ASSETS_DIR/icon.icns" "$APP_BUNDLE/Contents/Resources/AppIcon.icns"
  echo "  ✅ Icon copied"
else
  echo "  ⚠️  No icon.icns found, skipping icon"
fi

echo "  ✅ .app bundle assembled at: $APP_BUNDLE"

# ─── 3. Codesign ────────────────────────────────────────────────
echo ""
echo "📦 Step 3/6: Codesigning..."

ENTITLEMENTS_PATH="$MACOS_DIR/$APP_NAME.entitlements"

if [[ -n "${APPLE_CERT_BASE64:-}" ]]; then
  echo "  🔐 Signing with Apple Developer certificate..."
  codesign --force --deep --sign "$SIGNING_IDENTITY" \
    --entitlements "$ENTITLEMENTS_PATH" \
    --options runtime \
    --timestamp \
    "$APP_BUNDLE"
  echo "  ✅ Signed with identity: $SIGNING_IDENTITY"
else
  echo "  📝 Ad-hoc signing (no certificate configured)..."
  codesign --force --deep --sign - \
    --entitlements "$ENTITLEMENTS_PATH" \
    "$APP_BUNDLE"
  echo "  ✅ Ad-hoc signed"
fi

# Verify signature
codesign --verify --deep --strict "$APP_BUNDLE" 2>&1 && echo "  ✅ Signature verified" || echo "  ⚠️  Signature verification issue"

# ─── 4. Notarize (conditional) ──────────────────────────────────
echo ""
echo "📦 Step 4/6: Notarization..."

if [[ -n "${APPLE_CERT_BASE64:-}" ]] && [[ -n "${APPLE_ID:-}" ]] && [[ -n "${APPLE_TEAM_ID:-}" ]] && [[ -n "${APPLE_APP_PASSWORD:-}" ]]; then
  echo "  🔐 Submitting for notarization..."

  # Create a temporary ZIP for notarization
  NOTARIZE_ZIP="$DIST_DIR/notarize.zip"
  ditto -c -k --keepParent "$APP_BUNDLE" "$NOTARIZE_ZIP"

  # Submit for notarization
  xcrun notarytool submit "$NOTARIZE_ZIP" \
    --apple-id "$APPLE_ID" \
    --team-id "$APPLE_TEAM_ID" \
    --password "$APPLE_APP_PASSWORD" \
    --wait \
    --timeout 30m

  # Staple the notarization ticket
  xcrun stapler staple "$APP_BUNDLE"

  # Clean up
  rm -f "$NOTARIZE_ZIP"

  echo "  ✅ Notarized and stapled"
else
  echo "  ⏭️  Skipping notarization (secrets not configured)"
fi

# ─── 5. Create DMG ──────────────────────────────────────────────
echo ""
echo "📦 Step 5/6: Creating DMG..."

mkdir -p "$DIST_DIR"

if command -v create-dmg &> /dev/null; then
  create-dmg \
    --volname "$DISPLAY_NAME" \
    --volicon "$ASSETS_DIR/icon.icns" \
    --window-pos 200 120 \
    --window-size 600 400 \
    --icon-size 100 \
    --icon "$APP_NAME.app" 175 190 \
    --app-drop-link 425 190 \
    --hide-extension "$APP_NAME.app" \
    "$DMG_PATH" \
    "$APP_BUNDLE" || {
      # create-dmg may fail on CI if background image issues occur; fall back
      echo "  ⚠️  create-dmg failed, falling back to hdiutil..."
      rm -f "$DMG_PATH"
      hdiutil create -volname "$DISPLAY_NAME" \
        -srcfolder "$APP_BUNDLE" \
        -ov -format UDZO \
        "$DMG_PATH"
    }
else
  echo "  📝 Using hdiutil (create-dmg not available)..."
  hdiutil create -volname "$DISPLAY_NAME" \
    -srcfolder "$APP_BUNDLE" \
    -ov -format UDZO \
    "$DMG_PATH"
fi

echo "  ✅ DMG created: $DMG_PATH"
echo "     Size: $(du -h "$DMG_PATH" | cut -f1)"

# ─── 6. Summary ─────────────────────────────────────────────────
echo ""
echo "╔══════════════════════════════════════════════════════╗"
echo "║  Build complete!"
echo "║"
echo "║  App:       $APP_BUNDLE"
echo "║  DMG:       $DMG_PATH"
echo "║  Version:   $VERSION"
echo "║  Signed:    $([ -n "${APPLE_CERT_BASE64:-}" ] && echo 'Yes (Developer ID)' || echo 'Ad-hoc')"
echo "║  Notarized: $([ -n "${APPLE_CERT_BASE64:-}" ] && [ -n "${APPLE_ID:-}" ] && echo 'Yes' || echo 'No')"
echo "╚══════════════════════════════════════════════════════╝"
