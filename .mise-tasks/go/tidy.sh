#!/usr/bin/env bash
#MISE description="Run go mod tidy in every Go module"

set -e

# The root module and the glint module (the golangci-lint plugin, which
# deliberately does not share the root module's dependency graph).
go mod tidy
go -C glint mod tidy
