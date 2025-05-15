#!/bin/sh
# This script installs the latest version of the Render CLI
# You can run it directly:
#   curl -fsSL https://raw.githubusercontent.com/render-oss/cli/bin/install.sh | sh

set -e

# Prevent running with partial download
{ # this ensures the entire script is downloaded

# Function to get latest release info using GitHub API
get_latest_release() {
    curl --silent "https://api.github.com/repos/render-oss/cli/releases/latest" |
        sed -n 's/.*"tag_name": "\([^"]*\)".*/\1/p'
}

# Function to output error message and exit
error() {
    echo "Error: $1" >&2
    exit 1
}

# Check for required commands
command -v curl >/dev/null 2>&1 || error "curl is required but not installed"
command -v sed >/dev/null 2>&1 || error "sed is required but not installed"
command -v unzip >/dev/null 2>&1 || error "unzip is required but not installed"

# Detect OS
OS="$(uname -s)"
case "${OS}" in
    Linux*)     OS_NAME=linux;;
    Darwin*)    OS_NAME=darwin;;
    *)          error "Unsupported operating system: ${OS}";;
esac

# Detect architecture
ARCH="$(uname -m)"
case "${ARCH}" in
    x86_64*)    ARCH_NAME=amd64;;
    arm64*)     ARCH_NAME=arm64;;
    aarch64*)   ARCH_NAME=arm64;;
    *)          error "Unsupported architecture: ${ARCH}";;
esac

# Get the latest release version
VERSION=$(get_latest_release)
if [ -z "$VERSION" ]; then
    error "Failed to get latest release version"
fi

# Remove 'v' prefix from version if present
VERSION_NUM="${VERSION#v}"

echo "Installing Render CLI version ${VERSION}..."

# Construct download URL
BINARY_NAME="cli_${VERSION_NUM}_${OS_NAME}_${ARCH_NAME}.zip"
DOWNLOAD_URL="https://github.com/render-oss/cli/releases/download/${VERSION}/${BINARY_NAME}"

# Create temporary directory
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

# Download and install
echo "Downloading from ${DOWNLOAD_URL}..."
curl -fsSL "$DOWNLOAD_URL" -o "${TMP_DIR}/${BINARY_NAME}"

# Determine install location
if [ "$(id -u)" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
else
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
fi

# Unzip in temporary directory
unzip -o "${TMP_DIR}/${BINARY_NAME}" -d "${TMP_DIR}" >/dev/null 2>&1

# Find and move the binary
RENDER_BINARY=$(find "${TMP_DIR}" -type f -name "cli_v*" | head -n 1)
if [ -z "$RENDER_BINARY" ]; then
    error "Could not find CLI binary in the archive"
fi

mv "${RENDER_BINARY}" "${INSTALL_DIR}/render"
chmod +x "${INSTALL_DIR}/render"

# Verify installation by checking the binary directly
if [ -x "${INSTALL_DIR}/render" ]; then
    echo "✨ Successfully installed Render CLI to ${INSTALL_DIR}/render"
    echo
    if ! command -v render >/dev/null 2>&1; then
        echo "NOTE: Make sure ${INSTALL_DIR} is in your PATH by adding this to your shell's rc file:"
        echo "  export PATH=\$PATH:${INSTALL_DIR}"
        echo
        echo "To use render CLI immediately, run:"
        echo "  export PATH=\$PATH:${INSTALL_DIR}"
        echo "  ${INSTALL_DIR}/render --version"
    else
        "${INSTALL_DIR}/render" --version
    fi
else
    error "Installation failed: Could not install binary to ${INSTALL_DIR}/render"
fi

}
