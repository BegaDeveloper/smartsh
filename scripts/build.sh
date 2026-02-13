#!/usr/bin/env sh
set -eu

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
BIN_DIR="$DIST_DIR/bin"
RELEASE_DIR="$DIST_DIR/release"

mkdir -p "$BIN_DIR" "$RELEASE_DIR"
mkdir -p \
  "$BIN_DIR/smartsh_darwin_amd64" \
  "$BIN_DIR/smartsh_darwin_arm64" \
  "$BIN_DIR/smartsh_linux_amd64" \
  "$BIN_DIR/smartsh_linux_arm64" \
  "$BIN_DIR/smartsh_windows_amd64"

build_unix() {
  os="$1"
  arch="$2"
  out_dir="$3"
  echo "Building ${os} ${arch}..."
  GOOS="$os" GOARCH="$arch" go build -o "${out_dir}/smartsh" "$ROOT_DIR/cmd/smartsh"
  GOOS="$os" GOARCH="$arch" go build -o "${out_dir}/smartshd" "$ROOT_DIR/cmd/smartshd"
}

build_windows() {
  arch="$1"
  out_dir="$2"
  echo "Building windows ${arch}..."
  GOOS=windows GOARCH="$arch" go build -o "${out_dir}/smartsh.exe" "$ROOT_DIR/cmd/smartsh"
  GOOS=windows GOARCH="$arch" go build -o "${out_dir}/smartshd.exe" "$ROOT_DIR/cmd/smartshd"
}

build_unix darwin amd64 "$BIN_DIR/smartsh_darwin_amd64"
build_unix darwin arm64 "$BIN_DIR/smartsh_darwin_arm64"
build_unix linux amd64 "$BIN_DIR/smartsh_linux_amd64"
build_unix linux arm64 "$BIN_DIR/smartsh_linux_arm64"
build_windows amd64 "$BIN_DIR/smartsh_windows_amd64"

echo "Packaging archives..."
tar -czf "$RELEASE_DIR/smartsh_darwin_amd64.tar.gz" -C "$BIN_DIR/smartsh_darwin_amd64" smartsh smartshd
tar -czf "$RELEASE_DIR/smartsh_darwin_arm64.tar.gz" -C "$BIN_DIR/smartsh_darwin_arm64" smartsh smartshd
tar -czf "$RELEASE_DIR/smartsh_linux_amd64.tar.gz" -C "$BIN_DIR/smartsh_linux_amd64" smartsh smartshd
tar -czf "$RELEASE_DIR/smartsh_linux_arm64.tar.gz" -C "$BIN_DIR/smartsh_linux_arm64" smartsh smartshd
(
  cd "$BIN_DIR/smartsh_windows_amd64"
  if ! command -v zip >/dev/null 2>&1; then
    echo "zip command is required for packaging Windows artifact."
    exit 1
  fi
  zip -q "$RELEASE_DIR/smartsh_windows_amd64.zip" smartsh.exe smartshd.exe
)

echo "Writing checksums..."
if command -v shasum >/dev/null 2>&1; then
  (cd "$RELEASE_DIR" && shasum -a 256 smartsh_darwin_amd64.tar.gz smartsh_darwin_arm64.tar.gz smartsh_linux_amd64.tar.gz smartsh_linux_arm64.tar.gz smartsh_windows_amd64.zip > checksums.txt)
elif command -v sha256sum >/dev/null 2>&1; then
  (cd "$RELEASE_DIR" && sha256sum smartsh_darwin_amd64.tar.gz smartsh_darwin_arm64.tar.gz smartsh_linux_amd64.tar.gz smartsh_linux_arm64.tar.gz smartsh_windows_amd64.zip > checksums.txt)
else
  echo "No checksum tool found (shasum/sha256sum)."
  exit 1
fi

echo "Done. Release artifacts in $RELEASE_DIR"
