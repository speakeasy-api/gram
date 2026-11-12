#!/usr/bin/env bash

#MISE description="Regenerate the files derived from the authz registry (scope constants, typed refs and tables)"
#MISE dir="{{ config_root }}/server"

set -euo pipefail

# The first run writes tables_gen.go; the second recompiles the evaluator
# from it, so the dashboard cases it answers reflect the registry just edited.
go run ./cmd/authzgen
go run ./cmd/authzgen
