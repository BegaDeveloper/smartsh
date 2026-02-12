#!/usr/bin/env sh
set -eu

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
BIN_DIR="$DIST_DIR/bin"
RELEASE_DIR="$DIST_DIR/release"

mkdir -p "$BIN_DIR" "$RELEASE_DIR"
mkdir -p "$BIN_DIR/smartsh-darwin-amd64" "$BIN_DIR/smartsh-darwin-arm64" "$BIN_DIR/smartsh-windows-amd64"

echo "Building macOS amd64..."
GOOS=darwin GOARCH=amd64 go build -o "$BIN_DIR/smartsh-darwin-amd64/smartsh" "$ROOT_DIR/cmd/smartsh"

echo "Building macOS arm64..."
GOOS=darwin GOARCH=arm64 go build -o "$BIN_DIR/smartsh-darwin-arm64/smartsh" "$ROOT_DIR/cmd/smartsh"

echo "Building Windows amd64..."
GOOS=windows GOARCH=amd64 go build -o "$BIN_DIR/smartsh-windows-amd64/smartsh.exe" "$ROOT_DIR/cmd/smartsh"

echo "Packaging archives..."
tar -czf "$RELEASE_DIR/smartsh-darwin-amd64.tar.gz" -C "$BIN_DIR/smartsh-darwin-amd64" smartsh
tar -czf "$RELEASE_DIR/smartsh-darwin-arm64.tar.gz" -C "$BIN_DIR/smartsh-darwin-arm64" smartsh
(
  cd "$BIN_DIR/smartsh-windows-amd64"
  if ! command -v zip >/dev/null 2>&1; then
    echo "zip command is required for packaging Windows artifact."
    exit 1
  fi
  zip -q "$RELEASE_DIR/smartsh-windows-amd64.zip" smartsh.exe
)

echo "Writing checksums..."
if command -v shasum >/dev/null 2>&1; then
  (cd "$RELEASE_DIR" && shasum -a 256 smartsh-darwin-amd64.tar.gz smartsh-darwin-arm64.tar.gz smartsh-windows-amd64.zip > checksums.txt)
elif command -v sha256sum >/dev/null 2>&1; then
  (cd "$RELEASE_DIR" && sha256sum smartsh-darwin-amd64.tar.gz smartsh-darwin-arm64.tar.gz smartsh-windows-amd64.zip > checksums.txt)
else
  echo "No checksum tool found (shasum/sha256sum)."
  exit 1
fi

echo "Done. Release artifacts in $RELEASE_DIR"
