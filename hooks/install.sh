#!/bin/sh
set -e

# speakeasy-hooks installer for macOS and Linux.
#
# Designed for quick installs over the network and CI/CD:
#   curl -fsSL https://raw.githubusercontent.com/speakeasy-api/gram/main/hooks/install.sh | sh
#
# Environment overrides:
#   VERSION      release version to install (default: latest hooks@ release)
#   INSTALL_DIR  target directory (default: /usr/local/bin, falling back to
#                ~/.local/bin when not writable)

REPO="speakeasy-api/gram"
BINARY="speakeasy-hooks"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

fmt_error() {
  printf 'install.sh: %s\n' "$1" >&2
}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
darwin | linux) ;;
*)
  fmt_error "unsupported OS: $os (on Windows, use hooks/install.ps1)"
  exit 1
  ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch="amd64" ;;
aarch64 | arm64) arch="arm64" ;;
*)
  fmt_error "unsupported architecture: $arch"
  exit 1
  ;;
esac

version="${VERSION:-}"
if [ -z "$version" ]; then
  # Releases in this repository are shared across components; hooks releases
  # are tagged hooks@<version> and can sit pages deep under the more frequent
  # server/dashboard releases.
  page=1
  while [ -z "$version" ] && [ $page -le 10 ]; do
    body=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases?per_page=100&page=${page}")
    version=$(printf '%s' "$body" |
      grep -o '"tag_name": *"hooks@[^"]*"' |
      head -1 |
      sed 's/.*hooks@\([^"]*\)".*/\1/')
    case "$body" in
    *'"tag_name"'*) ;;
    *) break ;; # past the last page
    esac
    page=$((page + 1))
  done
fi
if [ -z "$version" ]; then
  fmt_error "could not resolve the latest ${BINARY} release"
  exit 1
fi

archive="${BINARY}_${os}_${arch}.zip"
url="https://github.com/${REPO}/releases/download/hooks%40${version}/${archive}"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

echo "Downloading ${BINARY} ${version} (${os}/${arch})..."
curl -fsSL --retry 5 --retry-delay 1 -o "${tmpdir}/${archive}" "$url"
unzip -q -o "${tmpdir}/${archive}" -d "$tmpdir"

bin_path=$(find "$tmpdir" -name "$BINARY" -type f | head -1)
if [ -z "$bin_path" ]; then
  fmt_error "archive did not contain ${BINARY}"
  exit 1
fi
chmod +x "$bin_path"

if [ ! -w "$INSTALL_DIR" ]; then
  INSTALL_DIR="${HOME}/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi
mv "$bin_path" "${INSTALL_DIR}/${BINARY}"

echo "Installed ${INSTALL_DIR}/${BINARY} ($("${INSTALL_DIR}/${BINARY}" --version))"
case ":$PATH:" in
*":${INSTALL_DIR}:"*) ;;
*) echo "Note: ${INSTALL_DIR} is not on your PATH." ;;
esac
