#!/usr/bin/env bash
set -e

# Korik P2P Chat Installation Script
# Usage: curl -sSL https://raw.githubusercontent.com/korikdev/korik/main/scripts/install.sh | bash

VERSION="v1.0.0"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

echo "=== Korik P2P Chat Installer ==="
echo "Target directory: ${INSTALL_DIR}"

mkdir -p "${INSTALL_DIR}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "${ARCH}" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: ${ARCH}"; exit 1 ;;
esac

echo "Detected OS: ${OS}, Arch: ${ARCH}"

# Build from source if go is available, otherwise attempt binary download
if command -v go >/dev/null 2>&1; then
    echo "Building korik from local Go environment..."
    go build -o "${INSTALL_DIR}/korik" .
else
    URL="https://github.com/korikdev/korik/releases/download/${VERSION}/korik-${OS}-${ARCH}"
    echo "Downloading binary from ${URL}..."
    curl -sSL "${URL}" -o "${INSTALL_DIR}/korik" || {
        echo "Error: Failed to download binary. Please install Go to build from source."
        exit 1
    }
fi

chmod +x "${INSTALL_DIR}/korik"
echo "Successfully installed korik to ${INSTALL_DIR}/korik"
if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
    echo "Note: Make sure ${INSTALL_DIR} is in your PATH."
fi
