#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Build the risk CEL engine to WASM for the dashboard editor"

set -e

# Codegen binaries are throwaway; skip VCS stamping for a stable, cacheable build.
export GOFLAGS="-buildvcs=false ${GOFLAGS:-}"

root=$(git rev-parse --show-toplevel)
out="$root/client/dashboard/public/cel"
mkdir -p "$out"

# The browser engine IS the server engine: same celenv, compiled to wasm. The
# editor loads this to type-check expressions and preview matched spans with no
# round-trip and no drift from save-time validation.
GOOS=js GOARCH=wasm go build -trimpath -o "$out/cel.wasm" ./cmd/celwasm

# wasm_exec.js is the Go runtime shim; it must come from the SAME toolchain that
# built cel.wasm, so copy it from GOROOT rather than vendoring a stale copy.
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$out/wasm_exec.js"

echo "built $out/cel.wasm ($(du -h "$out/cel.wasm" | cut -f1)) + wasm_exec.js"
