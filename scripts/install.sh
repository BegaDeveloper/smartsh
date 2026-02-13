#!/usr/bin/env sh
set -eu

# GitHub repo in the form "owner/name"
GITHUB_REPO="${SMARTSH_REPO:-BegaDeveloper/smartsh}"

# "latest" or a tag like "v1.2.3"
VERSION="${SMARTSH_VERSION:-latest}"

INSTALL_DIR="${SMARTSH_INSTALL_DIR:-/usr/local/bin}"

# Space-separated list of components to install from the release archive.
# Options: smartsh, smartshd
COMPONENTS="${SMARTSH_COMPONENTS:-smartsh}"

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

ASSET="smartsh_${OS}_${ARCH}.tar.gz"
CHECKSUMS="checksums.txt"

if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${GITHUB_REPO}/releases/latest/download"
else
  BASE_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}"
fi

TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

echo "Downloading ${BASE_URL}/${ASSET}"
curl -fsSL "${BASE_URL}/${ASSET}" -o "${TMP_DIR}/${ASSET}"
curl -fsSL "${BASE_URL}/${CHECKSUMS}" -o "${TMP_DIR}/${CHECKSUMS}"

expected="$(grep "  ${ASSET}\$" "${TMP_DIR}/${CHECKSUMS}" | awk '{print $1}' || true)"
if [ -z "$expected" ]; then
  echo "Checksum entry not found for ${ASSET} in ${CHECKSUMS}"
  exit 1
fi

if command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "${TMP_DIR}/${ASSET}" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "${TMP_DIR}/${ASSET}" | awk '{print $1}')"
else
  echo "No checksum tool found (shasum/sha256sum)."
  exit 1
fi

if [ "$expected" != "$actual" ]; then
  echo "Checksum mismatch for ${ASSET}"
  echo "Expected: ${expected}"
  echo "Actual:   ${actual}"
  exit 1
fi

mkdir -p "${TMP_DIR}/extract"
tar -xzf "${TMP_DIR}/${ASSET}" -C "${TMP_DIR}/extract"

mkdir -p "$INSTALL_DIR"
for component in $COMPONENTS; do
  src="${TMP_DIR}/extract/${component}"
  if [ ! -f "$src" ]; then
    echo "Component not found in archive: ${component}"
    exit 1
  fi
  dest="${INSTALL_DIR}/${component}"
  echo "Installing ${component} to ${dest}"
  if command -v install >/dev/null 2>&1; then
    install -m 0755 "$src" "$dest"
  else
    cp "$src" "$dest"
    chmod +x "$dest"
  fi
done

echo "smartsh installed successfully"
