#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE description="Generate the Pub/Sub topology Helm values consumed by the gram-infra chart"

set -euo pipefail

find ./infra/ -name "*.pb.go" -delete
buf generate
buf build -o ./infra/cmd/infra/descriptors.pb
go run ./infra/main.go gen-cc
