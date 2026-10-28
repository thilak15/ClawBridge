#!/bin/bash
set -e

# One-liner install script for ClawBridge
# Detects operating system and architecture, installs static binary

REPO="thilak15/ClawBridge"
VERSION="1.0.0"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64)
        ARCH="amd64"
        ;;
    arm64|aarch64)
        ARCH="arm64"
        ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

BIN_NAME="clawbridge-${OS}-${ARCH}"
if [ "$OS" = "windows" ]; then
    BIN_NAME="${BIN_NAME}.exe"
fi

INSTALL_DIR="/usr/local/bin"
if [ ! -w "$INSTALL_DIR" ]; then
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
fi

TARGET="${INSTALL_DIR}/clawbridge"
echo "Installing ClawBridge to ${TARGET}..."

# 1. If local binary exists in dist/ or bin/, use it
if [ -f "dist/${BIN_NAME}" ]; then
    cp "dist/${BIN_NAME}" "$TARGET"
elif [ -f "bin/clawbridge" ]; then
    cp "bin/clawbridge" "$TARGET"
else
    # 2. Download release binary from GitHub releases with fail flag (-f)
    URL="https://github.com/${REPO}/releases/download/v${VERSION}/${BIN_NAME}"
    echo "Downloading ${URL}..."
    TMP_FILE="$(mktemp)"
    if curl -sSLf "$URL" -o "$TMP_FILE"; then
        mv "$TMP_FILE" "$TARGET"
    else
        rm -f "$TMP_FILE"
        echo "Failed to download binary from GitHub Release."
        if command -v go >/dev/null 2>&1; then
            echo "Go detected. Compiling directly from source..."
            GOBIN="$INSTALL_DIR" go install github.com/${REPO}/cmd/clawbridge@latest
        else
            echo "Please download the binary manually from: https://github.com/${REPO}/releases"
            exit 1
        fi
    fi
fi

chmod +x "$TARGET"
echo "Successfully installed ClawBridge to ${TARGET}"
"$TARGET" --version
