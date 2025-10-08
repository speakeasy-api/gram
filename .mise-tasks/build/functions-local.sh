#!/usr/bin/env bash

#MISE description="Build Gram Functions runner images for local use"
#MISE dir="{{ config_root }}/functions"

#USAGE flag "--arch <arch>" help="Comma-separated list of target architectures (e.g. amd64,arm64). Defaults to the current architecture."

set -euo pipefail

arch="$(uname -m)"
if [ -n "${usage_arch:-}" ]; then
  arch="$usage_arch"
fi

arch="${arch/aarch64/arm64}"
arch="${arch/x86_64/amd64}"
echo "Building for architecture(s): $arch"

mise run build:functions-bin --arch "$arch" --dev
mise run build:functions-image \
  --arch "$arch" \
  --apko-config "./images/nodejs22-alpine3.22.yaml" \
  --image "gram-runner-nodejs22:dev" \
  --tarball-name "image.tar" \
  --out "./oci/nodejs22"

docker rmi --force "gram-runner-nodejs22:dev-$arch" gram-runner-nodejs22:dev || true
docker image load -i ./oci/nodejs22/image.tar
docker tag "gram-runner-nodejs22:dev-$arch" "gram-runner-nodejs22:dev"
docker rmi "gram-runner-nodejs22:dev-$arch"