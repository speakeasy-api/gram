#!/usr/bin/env bash

#MISE description="Regenerate the files derived from the authz registry (scope constants, typed refs and tables)"
#MISE dir="{{ config_root }}/server"

set -euo pipefail

go run ./cmd/authzgen
