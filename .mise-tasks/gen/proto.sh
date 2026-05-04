#!/usr/bin/env bash

#MISE description="Generate protobuf definitions"

set -euo pipefail

rm -rf protogen
buf generate
buf build -o server/cmd/gram/descriptors.pb
