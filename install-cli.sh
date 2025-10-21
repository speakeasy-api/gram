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

# Get the latest CLI release tag from GitHub
get_latest_tag() {
    info "Fetching latest release..."

    local tags_url="https://api.github.com/repos/speakeasy-api/gram/tags?per_page=100"
    local latest_tag

    if command_exists curl; then
        latest_tag=$(curl -sf "$tags_url" | grep -o '"name": *"cli/[^"]*"' | head -n1 | sed 's/"name": "cli\/\([^"]*\)"/\1/')
    elif command_exists wget; then
        latest_tag=$(wget -qO- "$tags_url" | grep -o '"name": *"cli/[^"]*"' | head -n1 | sed 's/"name": "cli\/\([^"]*\)"/\1/')
    else
        error "curl or wget is required"
    fi

    if [ -z "$latest_tag" ]; then
        error "Failed to fetch latest release tag"
    fi

    echo "$latest_tag"
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
    local install_path="$install_dir/gram"

    info "Installing gram to $install_path..."

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
    info "Installing Gram CLI..."

    # Detect system
    local os
    local arch
    os=$(detect_os)
    arch=$(detect_arch)

    info "Detected OS: $os"
    info "Detected architecture: $arch"

    # Get latest version
    local version
    version=$(get_latest_tag)
    info "Latest version: $version"

    # Construct download URLs
    local encoded_tag="cli%2F${version}"
    local filename="gram_${os}_${arch}.zip"
    local download_url="https://github.com/speakeasy-api/gram/releases/download/${encoded_tag}/${filename}"
    local checksums_url="https://github.com/speakeasy-api/gram/releases/download/${encoded_tag}/checksums.txt"

    # Create temporary directory
    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT

    # Download binary archive
    info "Downloading: $download_url"
    download "$download_url" "$tmp_dir/$filename"

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

    # Determine install location
    local install_dir
    if [ "$os" = "windows" ]; then
        install_dir="${PROGRAMFILES:-C:\\Program Files}\\gram"
        local binary_name="gram.exe"
    else
        install_dir="/usr/local/bin"
        local binary_name="gram"
    fi

    # Install binary
    install_binary "$tmp_dir/$binary_name" "$install_dir"

    # Verify installation
    if command_exists gram; then
        gram --version
        printf "\n${GREEN}Success!${NC} Gram CLI has been installed.\n"
        printf "Run 'gram --help' to get started.\n"
    else
        printf "\n${YELLOW}Note:${NC} You may need to add $install_dir to your PATH\n"
        printf "Run 'export PATH=\$PATH:$install_dir' or add it to your shell profile.\n"
    fi
}

main "$@"
