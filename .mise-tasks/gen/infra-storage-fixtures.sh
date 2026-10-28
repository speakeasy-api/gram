#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE description="Regenerate isolated Parquet interoperability fixtures"

#USAGE flag "--local" help="Use the pinned local Go plugin without contacting the BSR"

set -euo pipefail

# A temporary module supplies the same infrastructure options as real payloads.
# Fixture declarations never enter the application's deployment topology.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cp -R infra/internal/storagefixture/proto/. "$tmp/"
mkdir -p "$tmp/gcp/pubsub/v1"
cp infra/proto/gcp/pubsub/v1/options.proto "$tmp/gcp/pubsub/v1/"
buf build "$tmp" -o infra/internal/storagefixture/descriptors.pb
template='{"version":"v2","plugins":[{"remote":"buf.build/protocolbuffers/go:v1.36.11","out":"infra/internal/storagefixture/pb","opt":"paths=source_relative"}]}'
if [[ "${usage_local:-false}" == "true" ]]; then
  template='{"version":"v2","plugins":[{"local":"protoc-gen-go","strategy":"all","out":"infra/internal/storagefixture/pb","opt":"paths=source_relative"}]}'
fi
buf generate "$tmp" --path "$tmp/fixture" --template "$template"
go run ./infra/main.go gen-storage \
  --descriptors infra/internal/storagefixture/descriptors.pb \
  --proto-root infra/internal/storagefixture/proto \
  --out infra/internal/storagefixture \
  --package storagefixture \
  --import-path github.com/speakeasy-api/gram/infra/internal/storagefixture
