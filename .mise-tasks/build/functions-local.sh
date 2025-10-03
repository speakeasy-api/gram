#!/usr/bin/env bash

#MISE description="Build Gram Functions runner images for local use"
#MISE dir="{{ config_root }}/functions"

set -euo pipefail

arch="$(uname -m)"
if [ "$arch" = "aarch64" ]; then
  arch="arm64"
fi

mise run build:functions-bin --arch "$arch"
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