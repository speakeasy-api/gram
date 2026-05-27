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

runtime_image_hash="$(mise run hash:assistant-runtime-image)"
image="gram-assistant-runtime:${runtime_image_hash}"

echo "Building assistant runtime image for architecture(s): $arch"
docker build --platform "linux/${arch}" -f ./agents/runtime-image/Dockerfile -t "${image}" .

if [ -n "${GRAM_ASSISTANT_RUNTIME_OCI_IMAGE:-}" ] && [ "$arch" = "amd64" ]; then
  fly_image="${GRAM_ASSISTANT_RUNTIME_OCI_IMAGE}:${runtime_image_hash}"
  docker rmi "$fly_image" || true
  docker tag "${image}" "$fly_image"
  docker push "$fly_image"
  echo ""
  echo "Pushed image to Fly.io registry as:"
  echo "$fly_image"
  echo ""
fi

echo "Image available locally as:"
echo "${image}"
