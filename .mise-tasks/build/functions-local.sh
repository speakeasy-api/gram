#!/usr/bin/env bash

#MISE description="Build Gram Functions runner images for local use"
#MISE dir="{{ config_root }}/functions"

#USAGE flag "--arch <arch>" help="Comma-separated list of target architectures (e.g. amd64,arm64). Defaults to the current architecture."
#USAGE flag "--runtime <runtime>" help="Runtime to build (e.g. nodejs22, nodejs24). Defaults to nodejs22." default="nodejs22" {
#USAGE   choices "nodejs22" "nodejs24"
#USAGE }

set -euo pipefail

arch="$(uname -m)"
if [ -n "${usage_arch:-}" ]; then
  arch="$usage_arch"
fi

arch="${arch/aarch64/arm64}"
arch="${arch/x86_64/amd64}"
runtime="${usage_runtime?Error: runtime is required}"

# Map runtime to its apko config file
case "$runtime" in
  nodejs22) apko_config="./images/nodejs22-alpine3.22.yaml" ;;
  nodejs24) apko_config="./images/nodejs24-alpine3.23.yaml" ;;
  *) echo "Error: unknown runtime: $runtime"; exit 1 ;;
esac

echo "Building $runtime for architecture(s): $arch"

mise run build:functions-bin --arch "$arch" --dev
mise run build:functions-image \
  --arch "$arch" \
  --apko-config "$apko_config" \
  --image "gram-runner-$runtime:dev" \
  --tarball-name "image.tar" \
  --out "./oci/$runtime"

docker rmi --force "gram-runner-$runtime:dev-$arch" "gram-runner-$runtime:dev" || true
docker image load -i "./oci/$runtime/image.tar"
docker tag "gram-runner-$runtime:dev-$arch" "gram-runner-$runtime:dev"
docker rmi "gram-runner-$runtime:dev-$arch"

if [ -n "${GRAM_FUNCTIONS_RUNNER_OCI_IMAGE:-}" ] && [ "${GRAM_FUNCTIONS_RUNNER_VERSION:-}" = "dev" ] && [ "$arch" = "amd64" ]; then
  ver="${GRAM_FUNCTIONS_RUNNER_VERSION}"
  fly_image="${GRAM_FUNCTIONS_RUNNER_OCI_IMAGE}:${ver}-${runtime}"
  docker rmi "$fly_image" || true
  docker tag "gram-runner-$runtime:dev" "$fly_image"
  docker push "$fly_image"
  echo ""
  echo "Pushed image to Fly.io registry as:"
  echo "$fly_image"
  echo ""
fi

echo "Image available locally as:"
echo "gram-runner-$runtime:dev"
