#!/usr/bin/env bash

#MISE description="Print a deterministic content hash for the assistant runtime image sources"
#MISE dir="{{ config_root }}"

set -euo pipefail

# Git already content-addresses the tree under agents/ — the tree object's
# SHA-1 covers every tracked file's mode, name, and content recursively, and
# skips untracked junk (build artefacts, editor swap files) by construction.
# Truncated to 12 chars to keep the registry tag short.
git rev-parse HEAD:agents | cut -c1-12
