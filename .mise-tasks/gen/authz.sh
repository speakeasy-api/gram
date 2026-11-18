#!/usr/bin/env bash

#MISE description="Regenerate the files derived from the authz model (scope constants and typed refs)"
#MISE dir="{{ config_root }}/server"

set -euo pipefail

go run ./cmd/authzgen
