#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Regenerate isolated Parquet interoperability fixtures"

set -euo pipefail

# A temporary module supplies the same infrastructure options as real payloads.
# Fixture declarations never enter the application's deployment topology.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cp -R infra/internal/storagefixture/proto/. "$tmp/"
mkdir -p "$tmp/gcp/pubsub/v1"
cp infra/proto/gcp/pubsub/v1/options.proto "$tmp/gcp/pubsub/v1/"
buf build "$tmp" -o infra/internal/storagefixture/descriptors.pb
buf generate "$tmp" --path "$tmp/fixture" --template '{"version":"v2","plugins":[{"remote":"buf.build/protocolbuffers/go:v1.36.11","out":"infra/internal/storagefixture/pb","opt":"paths=source_relative"}]}'
go run ./infra/main.go gen-storage \
  --descriptors infra/internal/storagefixture/descriptors.pb \
  --proto-root infra/internal/storagefixture/proto \
  --out infra/internal/storagefixture \
  --package storagefixture \
  --import-path github.com/speakeasy-api/gram/infra/internal/storagefixture
