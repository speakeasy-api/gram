#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE description="Generate the Pub/Sub topology + usage mermaid diagram (docs/pubsub-topology.md)"
#MISE hide=true
#MISE quiet=true

set -euo pipefail

# Codegen binaries are throwaway; skip VCS stamping.
export GOFLAGS="-buildvcs=false ${GOFLAGS:-}"

# Joins the proto-declared topology (embedded descriptors) with ast-grep scans of
# the Go and Python call sites. ast-grep is provided by mise (see mise.toml).
out="./docs/pubsub-topology.md"
go run ./infra/main.go gen-diagram --out "$out"

# Match the repo's markdown style (oxfmt pads tables) so the generated file is
# stable under the pre-commit formatter and re-runs are byte-identical.
pnpm exec oxfmt --write "$out"
