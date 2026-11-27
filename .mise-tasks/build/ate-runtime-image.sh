#!/usr/bin/env bash

#MISE description="Build the ATE runtime image for local use"
#MISE dir="{{ config_root }}"

#USAGE flag "--arch <arch>" help="Target architecture (e.g. amd64, arm64). Defaults to the current architecture."
#USAGE flag "--tag <tag>" default="gram-ate-runtime:dev" help="Image tag to build locally."

set -euo pipefail

arch="$(uname -m)"
if [ -n "${usage_arch:-}" ]; then
  arch="$usage_arch"
fi

arch="${arch/aarch64/arm64}"
arch="${arch/x86_64/amd64}"

image="${usage_tag:-gram-ate-runtime:dev}"

echo "Building ATE runtime image for architecture: $arch"
docker build --platform "linux/${arch}" -f ./agents/ate-runtime/Dockerfile -t "${image}" .

echo "Image available locally as:"
echo "${image}"
