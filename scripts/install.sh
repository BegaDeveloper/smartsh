#!/usr/bin/env sh
set -eu

REPO="${SMARTSH_REPO:-https://example.com/smartsh}"
VERSION="${SMARTSH_VERSION:-latest}"
INSTALL_DIR="${SMARTSH_INSTALL_DIR:-/usr/local/bin}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

if [ "$OS" != "darwin" ] && [ "$OS" != "linux" ]; then
  echo "Unsupported OS for this installer: $OS"
  exit 1
fi

if [ "$VERSION" = "latest" ]; then
  ASSET="smartsh-${OS}-${ARCH}"
else
  ASSET="smartsh-${OS}-${ARCH}"
fi

TMP_BIN="$(mktemp)"
URL="$REPO/releases/$VERSION/$ASSET"

echo "Downloading $URL"
curl -fsSL "$URL" -o "$TMP_BIN"
chmod +x "$TMP_BIN"

echo "Installing to $INSTALL_DIR/smartsh"
mkdir -p "$INSTALL_DIR"
mv "$TMP_BIN" "$INSTALL_DIR/smartsh"

echo "smartsh installed successfully"
