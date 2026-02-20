#!/usr/bin/env bash
# Install Gram Claude hooks - wrapper for the standalone install script
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Run the standalone install script from the repo
exec "$REPO_ROOT/hooks/install.sh"
