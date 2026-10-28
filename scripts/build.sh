#!/bin/bash
set -e

# Cross-platform static binary compilation script for ClawBridge
# Zero CGO dependencies, 100% portable static binaries

VERSION="1.0.0"
OUTPUT_DIR="dist"
mkdir -p "$OUTPUT_DIR"

PLATFORMS=(
    "darwin/arm64"
    "darwin/amd64"
    "linux/amd64"
    "linux/arm64"
    "windows/amd64"
)

echo "Building ClawBridge v${VERSION} static binaries (pure Go, zero CGO)..."

for PLATFORM in "${PLATFORMS[@]}"; do
    GOOS=${PLATFORM%/*}
    GOARCH=${PLATFORM#*/}
    BIN_NAME="clawbridge-${GOOS}-${GOARCH}"
    if [ "$GOOS" = "windows" ]; then
        BIN_NAME="${BIN_NAME}.exe"
    fi

    echo "Compiling for ${GOOS}/${GOARCH} -> ${OUTPUT_DIR}/${BIN_NAME}..."
    CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build \
        -ldflags="-s -w" \
        -trimpath \
        -o "${OUTPUT_DIR}/${BIN_NAME}" \
        ./cmd/clawbridge

    # Generate sha256 checksum
    if command -v shasum >/dev/null 2>&1; then
        (cd "$OUTPUT_DIR" && shasum -a 256 "$BIN_NAME" > "${BIN_NAME}.sha256")
    fi
done

echo "Build complete. Artifacts in ${OUTPUT_DIR}/:"
ls -lh "$OUTPUT_DIR"
