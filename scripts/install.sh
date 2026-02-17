#!/usr/bin/env sh
set -eu

# GitHub repo in the form "owner/name"
GITHUB_REPO="${SMARTSH_REPO:-BegaDeveloper/smartsh}"

# "latest" or a tag like "v1.2.3"
VERSION="${SMARTSH_VERSION:-latest}"

INSTALL_DIR="${SMARTSH_INSTALL_DIR:-/usr/local/bin}"

# Space-separated list of components to install from the release archive.
# Options: smartsh, smartshd
COMPONENTS="${SMARTSH_COMPONENTS:-smartsh smartshd}"

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

CHECKSUMS="checksums.txt"

if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${GITHUB_REPO}/releases/latest/download"
else
  BASE_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}"
fi

TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

SUDO=""
if [ "$(id -u)" -ne 0 ] && [ ! -w "$INSTALL_DIR" ]; then
  if command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  else
    echo "Install dir is not writable: $INSTALL_DIR" >&2
    echo "Tip: set SMARTSH_INSTALL_DIR to a user-writable directory, e.g.:" >&2
    echo "  SMARTSH_INSTALL_DIR=\"\$HOME/.local/bin\" $0" >&2
    exit 1
  fi
fi

# Support both underscore and hyphen artifact naming styles.
ASSET_CANDIDATES="
smartsh_${OS}_${ARCH}.tar.gz
smartsh-${OS}-${ARCH}.tar.gz
"

ASSET=""
for candidate in $ASSET_CANDIDATES; do
  if curl -fsI "${BASE_URL}/${candidate}" >/dev/null 2>&1; then
    ASSET="$candidate"
    break
  fi
done

if [ -z "$ASSET" ]; then
  echo "No compatible release asset found for OS=${OS} ARCH=${ARCH}" >&2
  exit 1
fi

echo "Downloading ${BASE_URL}/${ASSET}"
curl -fLsS "${BASE_URL}/${ASSET}" -o "${TMP_DIR}/${ASSET}"
curl -fLsS "${BASE_URL}/${CHECKSUMS}" -o "${TMP_DIR}/${CHECKSUMS}"

# Allow checksum entry to use either naming style.
alt_asset="$(printf "%s" "$ASSET" | tr '_-' '-_')"
expected="$(grep "  ${ASSET}\$" "${TMP_DIR}/${CHECKSUMS}" | awk '{print $1}' || true)"
if [ -z "$expected" ]; then
  expected="$(grep "  ${alt_asset}\$" "${TMP_DIR}/${CHECKSUMS}" | awk '{print $1}' || true)"
fi
if [ -z "$expected" ]; then
  echo "Checksum entry not found for ${ASSET} in ${CHECKSUMS}" >&2
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

$SUDO mkdir -p "$INSTALL_DIR"
for component in $COMPONENTS; do
  src="${TMP_DIR}/extract/${component}"
  if [ ! -f "$src" ]; then
    echo "Component not found in archive: ${component}"
    exit 1
  fi
  dest="${INSTALL_DIR}/${component}"
  echo "Installing ${component} to ${dest}"
  if command -v install >/dev/null 2>&1; then
    $SUDO install -m 0755 "$src" "$dest"
  else
    $SUDO cp "$src" "$dest"
    $SUDO chmod +x "$dest"
  fi
done

echo "smartsh installed successfully."
echo "Installed: $COMPONENTS"
echo "Location:  $INSTALL_DIR"
if [ "$INSTALL_DIR" = "$HOME/.local/bin" ] || [ "$INSTALL_DIR" = "$HOME/bin" ]; then
  echo "Tip: ensure your PATH includes $INSTALL_DIR"
fi

SMARTSH_BIN="${INSTALL_DIR}/smartsh"
if [ -x "$SMARTSH_BIN" ]; then
  echo "Running one-time setup: smartsh setup-agent"
  if "$SMARTSH_BIN" setup-agent; then
    echo "setup-agent completed."
  else
    echo "WARN: setup-agent failed during installer run." >&2
    echo "      You can retry later with: smartsh setup-agent" >&2
  fi
fi
