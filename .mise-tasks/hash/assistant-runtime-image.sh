#!/usr/bin/env bash

#MISE description="Print a deterministic content hash for the assistant runtime image sources"
#MISE dir="{{ config_root }}"

set -euo pipefail

# Hash every file under agents/ — the Dockerfile copies from both
# agents/runner/ (Rust runner source) and agents/runtime-image/ (init scripts,
# sandbox, Dockerfile itself with its pinned base image digests). Build
# artefacts under target/ and node_modules/ never reach the image, so we skip
# them to keep the hash stable across local builds.
find agents -type f \
    -not -path '*/target/*' \
    -not -path '*/node_modules/*' \
    -print0 \
    | LC_ALL=C sort -z \
    | xargs -0 shasum -a 256 \
    | shasum -a 256 \
    | cut -c1-12
