#!/usr/bin/env bash

#MISE description="Regenerate the files derived from the authz model (scope constants)"
#MISE dir="{{ config_root }}/server"

set -euo pipefail

go run ./cmd/authzgen
