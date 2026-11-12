#!/usr/bin/env bash

#MISE description="Regenerate the files derived from the authz registry (scope constants, typed refs and tables)"
#MISE dir="{{ config_root }}/server"

set -euo pipefail

# authzgen reads only the registry, so it runs even when the generated files
# are missing or stale. authzcases then answers the dashboard's cases with the
# evaluator built from the files authzgen just wrote.
go run ./cmd/authzgen
go run ./cmd/authzcases
