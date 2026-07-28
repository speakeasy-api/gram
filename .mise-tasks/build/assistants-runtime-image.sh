#!/usr/bin/env bash

#MISE description="Build the assistant runtime image for local use"
#MISE dir="{{ config_root }}"

#USAGE flag "--arch <arch>" help="Comma-separated list of target architectures (e.g. amd64,arm64). Defaults to the current architecture."

set -euo pipefail

arch="$(uname -m)"
if [ -n "${usage_arch:-}" ]; then
  arch="$usage_arch"
fi

arch="${arch/aarch64/arm64}"
arch="${arch/x86_64/amd64}"

image="${GRAM_ASSISTANT_RUNTIME_OCI_IMAGE:-gram-assistant-runtime}:dev"

echo "Building assistant runtime image for architecture(s): $arch"
docker build --platform "linux/${arch}" -f ./agents/runtime-image/Dockerfile -t "${image}" .

echo "Image available locally as:"
echo "${image}"
echo ""
echo "The local assistant runtime provider picks it up on the next admission"
echo "or recycle — no registry push needed."
