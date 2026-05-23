#!/usr/bin/env bash
set -euo pipefail

BINARY_NAME="codebuddy-proxy"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo none)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-s -w -H=windowsgui -X codebuddy-proxy/internal/version.Version=${VERSION} -X codebuddy-proxy/internal/version.Commit=${COMMIT} -X codebuddy-proxy/internal/version.Date=${DATE}"

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
    go build -ldflags "$LDFLAGS" -o "${BINARY_NAME}.exe" ./cmd/proxy

echo "Built ${BINARY_NAME}.exe successfully"
