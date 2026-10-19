#!/usr/bin/env bash
# Values for x_defs stamping (see server/BUILD.bazel). Bazel runs this on every
# build with --stamp; STABLE_ keys invalidate stamped outputs when they change.
set -euo pipefail

echo "STABLE_GIT_SHA $(git rev-parse HEAD)"
echo "STABLE_ASSISTANT_RUNTIME_IMAGE_HASH ${GRAM_ASSISTANT_RUNTIME_IMAGE_HASH:-dev}"
