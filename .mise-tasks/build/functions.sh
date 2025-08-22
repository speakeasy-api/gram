#!/usr/bin/env bash

#MISE description="Build the gram functions runner"
#MISE dir="{{ config_root }}/functions"
#MISE sources=["melange.yaml", "go.mod", "go.sum", "**/*.go", "images/**/*.yaml"]
#MISE outputs={ auto = true }
#MISE depends=["go:tidy"]

set -euo pipefail

mkdir -p ./local ./oci

key_file="./local/melange-signing-key.rsa"
if [ ! -f "$key_file" ]; then
  echo "Generating signing key for melange..."
  mkdir -p ./local
  melange keygen "$key_file"
fi

rm -rf ./packages
melange build \
  --cache-dir "$(go env GOMODCACHE)" \
  --arch amd64,arm64,aarch64 \
  --runner docker \
  --git-repo-url https://github.com/speakeasy-api/gram \
  --signing-key ./local/melange-signing-key.rsa \
  ./config.yaml

node_image_path=./oci/gram-funcs-node
mkdir -p "$node_image_path"

apko build \
  --keyring-append "$key_file.pub" \
  --sbom-path "$node_image_path" \
  ./images/nodejs22-alpine3.22.yaml \
  gram-funcs-node:0.0.0-alpine3.22 \
  "$node_image_path/image.tar"