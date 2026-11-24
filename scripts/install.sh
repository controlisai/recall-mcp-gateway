#!/bin/sh
# Recall MCP Gateway installer
# Usage: curl -fsSL https://get.controlis.ai/proxy | sh
#
# Installs the recall-mcp-gateway binary for your platform.
# Requires: curl, tar (or unzip on Windows)

set -e

BINARY="recall-mcp-gateway"
REPO_URL="https://releases.controlis.ai/recall-mcp-gateway"
INSTALL_DIR="/usr/local/bin"

# Colors (if terminal supports them)
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

info() { printf "${BLUE}${BOLD}==>${NC} %s\n" "$1"; }
success() { printf "${GREEN}${BOLD}✓${NC} %s\n" "$1"; }
error() { printf "${RED}${BOLD}✗${NC} %s\n" "$1" >&2; exit 1; }

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin|linux) ;;
  mingw*|msys*|cygwin*) OS="windows" ;;
  *) error "Unsupported OS: $OS" ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) error "Unsupported architecture: $ARCH" ;;
esac

info "Detected platform: ${OS}/${ARCH}"

# Get latest version (fallback to "latest" if API unavailable)
VERSION=""
if command -v curl >/dev/null 2>&1; then
  VERSION=$(curl -fsSL "${REPO_URL}/latest.txt" 2>/dev/null || echo "")
fi
if [ -z "$VERSION" ]; then
  VERSION="latest"
fi

info "Installing Recall MCP Gateway ${VERSION}..."

# Download
DOWNLOAD_URL="${REPO_URL}/${VERSION}/${BINARY}-${OS}-${ARCH}.tar.gz"
if [ "$OS" = "windows" ]; then
  DOWNLOAD_URL="${REPO_URL}/${VERSION}/${BINARY}-${OS}-${ARCH}.zip"
fi

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

info "Downloading from ${DOWNLOAD_URL}..."
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$DOWNLOAD_URL" -o "${TMP_DIR}/archive"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "${TMP_DIR}/archive" "$DOWNLOAD_URL"
else
  error "curl or wget required"
fi

# Extract
info "Extracting..."
if [ "$OS" = "windows" ]; then
  unzip -q "${TMP_DIR}/archive" -d "$TMP_DIR"
else
  tar -xzf "${TMP_DIR}/archive" -C "$TMP_DIR"
fi

# Find the binary
BINARY_PATH=$(find "$TMP_DIR" -name "${BINARY}*" -type f ! -name "*.tar.gz" ! -name "*.zip" | head -1)
if [ -z "$BINARY_PATH" ]; then
  error "Binary not found in download"
fi
chmod +x "$BINARY_PATH"

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv "$BINARY_PATH" "${INSTALL_DIR}/${BINARY}"
elif command -v sudo >/dev/null 2>&1; then
  info "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mv "$BINARY_PATH" "${INSTALL_DIR}/${BINARY}"
else
  # Fallback to user-local install
  INSTALL_DIR="${HOME}/.local/bin"
  mkdir -p "$INSTALL_DIR"
  mv "$BINARY_PATH" "${INSTALL_DIR}/${BINARY}"
  info "Installed to ${INSTALL_DIR} (add to PATH if needed)"
fi

# Verify
if command -v "$BINARY" >/dev/null 2>&1; then
  INSTALLED_VERSION=$("$BINARY" --version 2>/dev/null || echo "unknown")
  success "Recall MCP Gateway installed: ${INSTALLED_VERSION}"
else
  success "Recall MCP Gateway installed to ${INSTALL_DIR}/${BINARY}"
fi

echo ""
echo "  ${BOLD}Quick start:${NC}"
echo "    ${BINARY} --from-claude-config"
echo ""
echo "  ${BOLD}Dashboard:${NC}"
echo "    http://localhost:8090/dashboard"
echo ""
echo "  ${BOLD}Docs:${NC}"
echo "    https://controlis.ai/#/docs/deployment"
echo ""
