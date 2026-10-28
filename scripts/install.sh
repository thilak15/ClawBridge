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

echo "Installing ClawBridge to ${INSTALL_DIR}..."

# If local binary exists in bin/ or dist/, copy it
if [ -f "dist/${BIN_NAME}" ]; then
    cp "dist/${BIN_NAME}" "${INSTALL_DIR}/clawbridge"
elif [ -f "bin/clawbridge" ]; then
    cp "bin/clawbridge" "${INSTALL_DIR}/clawbridge"
else
    # Download release binary from GitHub releases
    URL="https://github.com/${REPO}/releases/download/v${VERSION}/${BIN_NAME}"
    echo "Downloading ${URL}..."
    curl -sSL "$URL" -o "${INSTALL_DIR}/clawbridge"
fi

chmod +x "${INSTALL_DIR}/clawbridge"
echo "Successfully installed ClawBridge to ${INSTALL_DIR}/clawbridge"
"${INSTALL_DIR}/clawbridge" --version

