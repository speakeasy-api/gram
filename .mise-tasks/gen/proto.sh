#!/usr/bin/env bash

#MISE description="Generate protobuf definitions"

set -euo pipefail

find infra/ -name "*.pb.go" -delete
buf generate
buf build -o server/cmd/gram/descriptors.pb
