#!/bin/bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[1;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Global variables
tmp_dir=""

# Printed by `speakeasy --control-plane-cli`. The Speakeasy SDK generator CLI
# also installs a `speakeasy` binary and rejects that flag.
CONTROL_PLANE_MARKER="speakeasy-ai-control-plane-cli"

# Utility functions
info() {
    printf "${BLUE}==>${NC} %s\n" "$1" >&2
}

error() {
    printf "${RED}Error:${NC} %s\n" "$1" >&2
    exit 1
}

warn() {
    printf "${YELLOW}Warning:${NC} %s\n" "$1" >&2
}

# Check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Darwin*)
            echo "darwin"
            ;;
        Linux*)
            echo "linux"
            ;;
        MINGW*|MSYS*|CYGWIN*)
            echo "windows"
            ;;
        *)
            error "Unsupported operating system: $(uname -s)"
            ;;
    esac
}

# Detect architecture
detect_arch() {
    local arch
    arch="$(uname -m)"

    case "$arch" in
        x86_64|amd64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        i386|i686)
            echo "386"
            ;;
        *)
            error "Unsupported architecture: $arch"
            ;;
    esac
}

# Get the latest CLI tag from package.json (name@version)
get_latest_tag() {
    info "Fetching latest version..."

    local package_url="https://raw.githubusercontent.com/speakeasy-api/gram/refs/heads/main/cli/package.json"
    local response

    if command_exists curl; then
        response=$(curl -sf "$package_url")
    elif command_exists wget; then
        response=$(wget -qO- "$package_url")
    else
        error "curl or wget is required"
    fi

    if [ -z "$response" ]; then
        error "Failed to fetch package.json from GitHub"
    fi

    # Extract name and version from package.json
    local name
    local version
    name=$(echo "$response" | grep -oE '"name": *"[^"]*"' | sed -E 's/"name": "([^"]*)"/\1/')
    version=$(echo "$response" | grep -oE '"version": *"[^"]*"' | sed -E 's/"version": "([^"]*)"/\1/')

    if [ -z "$name" ] || [ -z "$version" ]; then
        error "Failed to extract name or version from package.json"
    fi

    # Construct and return tag as name@version
    echo "${name}@${version}"
}

# Download file
download() {
    local url="$1"
    local output="$2"

    if command_exists curl; then
        curl -fsSL "$url" -o "$output"
    elif command_exists wget; then
        wget -q "$url" -O "$output"
    else
        error "curl or wget is required"
    fi
}

# Download a release asset. Returns 0 on success and 2 when the server answers
# 404. Any other failure stops the script with the real error.
download_asset() {
    local url="$1"
    local output="$2"
    local status

    if command_exists curl; then
        status=$(curl -sSL -w '%{http_code}' -o "$output" "$url") || error "Failed to download $url"
    elif command_exists wget; then
        if wget -q "$url" -O "$output"; then
            status=200
        else
            status=$(wget --spider --server-response "$url" 2>&1 | awk '/^  HTTP\//{code=$2} END{print code}')
        fi
    else
        error "curl or wget is required"
    fi

    case "$status" in
        200) return 0 ;;
        404) return 2 ;;
        *) error "Failed to download $url (HTTP ${status:-unknown})" ;;
    esac
}

# Report whether the binary at $1 is the Speakeasy AI Control Plane CLI.
is_control_plane_cli() {
    "$1" --control-plane-cli 2>/dev/null | grep -qx "$CONTROL_PLANE_MARKER"
}

# Report whether the binary at $1 is a build of this CLI from before the
# rename. Those builds have no marker flag and print "gram version ...".
is_legacy_cli() {
    "$1" --version 2>/dev/null | grep -q '^gram version '
}

# Verify checksum
verify_checksum() {
    local file="$1"
    local checksums_file="$2"
    local filename="$3"

    info "Verifying checksum..."

    # Extract the expected checksum for our file
    local expected_checksum
    expected_checksum=$(grep "$filename" "$checksums_file" | awk '{print $1}')

    if [ -z "$expected_checksum" ]; then
        error "Checksum not found for $filename"
    fi

    # Calculate actual checksum
    local actual_checksum
    if command_exists shasum; then
        actual_checksum=$(shasum -a 256 "$file" | awk '{print $1}')
    elif command_exists sha256sum; then
        actual_checksum=$(sha256sum "$file" | awk '{print $1}')
    else
        error "Cannot verify checksum: shasum or sha256sum not found"
    fi

    if [ "$expected_checksum" != "$actual_checksum" ]; then
        error "Checksum verification failed!\nExpected: $expected_checksum\nActual: $actual_checksum"
    fi

    info "Checksum verified successfully"
}

# Install binary
install_binary() {
    local binary="$1"
    local install_dir="$2"
    local install_path="$install_dir/$3"

    info "Installing speakeasy to $install_path..."

    # Check if we need sudo
    local use_sudo=""
    if [ ! -w "$install_dir" ]; then
        if command_exists sudo; then
            warn "Root permissions required to install to $install_dir"
            use_sudo="sudo"
        else
            error "Cannot write to $install_dir and sudo is not available"
        fi
    fi

    # Create install directory if it doesn't exist
    if [ ! -d "$install_dir" ]; then
        $use_sudo mkdir -p "$install_dir"
    fi

    # Move binary
    $use_sudo mv "$binary" "$install_path"
    $use_sudo chmod +x "$install_path"

    info "Installation complete!"
}

# Main installation logic
main() {
    info "Installing the speakeasy CLI..."

    # Detect system
    local os
    local arch
    os=$(detect_os)
    arch=$(detect_arch)

    info "Detected OS: $os"
    info "Detected architecture: $arch"

    # Get latest tag (name@version)
    local tag_name
    tag_name=$(get_latest_tag)
    info "Latest version: $tag_name"

    # Determine install location. INSTALL_DIR overrides the default.
    local install_dir
    local install_name
    if [ "$os" = "windows" ]; then
        install_dir="${INSTALL_DIR:-${PROGRAMFILES:-C:\\Program Files}\\speakeasy}"
        install_name="speakeasy.exe"
    else
        install_dir="${INSTALL_DIR:-/usr/local/bin}"
        install_name="speakeasy"
    fi
    local install_path="$install_dir/$install_name"

    # Never overwrite the Speakeasy SDK generator CLI, which installs a binary
    # with the same name.
    if [ -e "$install_path" ] && ! is_control_plane_cli "$install_path" && ! is_legacy_cli "$install_path"; then
        error "$install_path already exists and is not the Speakeasy AI Control Plane CLI. It looks like the Speakeasy SDK CLI, which also installs a 'speakeasy' binary. Install to another directory by setting INSTALL_DIR, for example: curl -fsSL https://go.getgram.ai/cli.sh | INSTALL_DIR=\"\$HOME/.local/bin\" bash"
    fi

    # Construct download URLs. Releases made before the CLI was renamed only
    # publish gram archives, so fall back to those when the speakeasy archive
    # does not exist.
    local release_url="https://github.com/speakeasy-api/gram/releases/download/${tag_name}"
    local archive_name="speakeasy"
    local filename="${archive_name}_${os}_${arch}.zip"
    local checksums_url="${release_url}/checksums.txt"

    # Create temporary directory
    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT

    # Download binary archive
    info "Downloading: ${release_url}/${filename}"
    local status=0
    download_asset "${release_url}/${filename}" "$tmp_dir/$filename" || status=$?
    if [ "$status" -eq 2 ]; then
        archive_name="gram"
        filename="${archive_name}_${os}_${arch}.zip"
        info "No speakeasy archive in ${tag_name}. Downloading: ${release_url}/${filename}"
        status=0
        download_asset "${release_url}/${filename}" "$tmp_dir/$filename" || status=$?
        if [ "$status" -ne 0 ]; then
            error "No CLI archive for ${os}/${arch} in ${tag_name}"
        fi
    fi

    # Download checksums
    info "Downloading checksums..."
    download "$checksums_url" "$tmp_dir/checksums.txt"

    # Verify checksum
    verify_checksum "$tmp_dir/$filename" "$tmp_dir/checksums.txt" "$filename"

    # Extract binary
    info "Extracting binary..."
    if ! command_exists unzip; then
        error "unzip is required to extract the binary"
    fi

    unzip -q "$tmp_dir/$filename" -d "$tmp_dir"

    local binary_name="${archive_name}"
    if [ "$os" = "windows" ]; then
        binary_name="${archive_name}.exe"
    fi

    # Install binary
    install_binary "$tmp_dir/$binary_name" "$install_dir" "$install_name"

    # Verify the binary just installed, not whichever speakeasy is first on
    # PATH. Builds from before the rename have no marker flag.
    "$install_path" --version
    if [ "$archive_name" = "speakeasy" ] && ! is_control_plane_cli "$install_path"; then
        error "$install_path did not identify as the Speakeasy AI Control Plane CLI"
    fi
    printf "\n%bSuccess!%b The speakeasy CLI has been installed to %s.\n" "$GREEN" "$NC" "$install_path"

    local on_path
    on_path=$(command -v speakeasy 2>/dev/null || true)
    if [ -z "$on_path" ]; then
        printf "\n%bNote:%b You may need to add %s to your PATH\n" "$YELLOW" "$NC" "$install_dir"
        printf "Run 'export PATH=\$PATH:%s' or add it to your shell profile.\n" "$install_dir"
    elif [ "$on_path" != "$install_path" ]; then
        warn "'speakeasy' on your PATH resolves to $on_path, not $install_path. That may be the Speakeasy SDK CLI. Put $install_dir earlier on your PATH or run $install_path directly."
    else
        printf "Run 'speakeasy --help' to get started.\n"
    fi
}

main "$@"
