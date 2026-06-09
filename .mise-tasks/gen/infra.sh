#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE description="Generate the Pub/Sub topology Helm values consumed by the gram-infra chart"

set -euo pipefail

find ./infra/ -name "*.pb.go" -delete
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
