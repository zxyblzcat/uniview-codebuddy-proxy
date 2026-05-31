#!/usr/bin/env bash
set -euo pipefail

BINARY_NAME="uniview-codebuddy-proxy"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo none)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-s -w -H=windowsgui -X uniview-codebuddy-proxy/internal/version.Version=${VERSION} -X uniview-codebuddy-proxy/internal/version.Commit=${COMMIT} -X uniview-codebuddy-proxy/internal/version.Date=${DATE}"

ARCH=${1:-amd64}
case "$ARCH" in
    amd64) GOARCH="amd64" ;;
    arm64) GOARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "Building ${BINARY_NAME}.exe for windows/${GOARCH}..."

# Generate Windows resource (.syso) with icon if go-winres is available
if command -v go-winres &>/dev/null; then
    echo "Generating Windows resource with icon..."
    (cd cmd/proxy && go-winres make --arch "$GOARCH" --product-version git-tag --file-version git-tag)
fi

CGO_ENABLED=1 GOOS=windows GOARCH=$GOARCH \
    go build -tags gui -ldflags "$LDFLAGS" -o "${BINARY_NAME}.exe" ./cmd/proxy

# Compress with UPX if available
# 使用 -5 而非 --best：--best 耗时 ~280s 只比 -5 多省 ~8%
if command -v upx &>/dev/null; then
    echo "Compressing with UPX..."
    upx -5 "${BINARY_NAME}.exe" 2>/dev/null || true
else
    echo "UPX not found, skipping compression (install with: brew install upx)"
fi

echo "Built ${BINARY_NAME}.exe successfully"
echo "Size: $(ls -lh "${BINARY_NAME}.exe" | awk '{print $5}')"
