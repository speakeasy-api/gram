#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE description="Generate the Pub/Sub topology Helm values consumed by the gram-infra chart"

set -euo pipefail

# Codegen binaries are throwaway; skip VCS stamping.
export GOFLAGS="-buildvcs=false ${GOFLAGS:-}"

# Prune stale codegen for both languages before regenerating: buf has no clean
# option, so a deleted or renamed proto would otherwise leave orphaned modules
# behind (committed, shipped in the Python wheel, and invisible to CI's
# porcelain check).
# .venv is excluded: site-packages is full of installed *_pb2.py modules
# (protobuf itself, googleapis-common-protos, ...) that this sweep must never
# touch. (-not -path rather than -prune: -delete implies -depth, which
# disables -prune.)
find ./infra/ -not -path "*/.venv/*" \( -name "*.pb.go" -o -name "*_pb2.py" -o -name "*_pb2.pyi" \) -delete
buf generate

buf build -o ./infra/gen/descriptors.pb
cat > ./infra/gen/descriptors.go <<EOF
package gen

import _ "embed"

//go:embed descriptors.pb
var Descriptors []byte
EOF
go fmt ./infra/gen/descriptors.go

go run ./infra/main.go gen-cc
