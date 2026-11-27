#!/usr/bin/env bash

#MISE description="Print a deterministic content hash for the ATE runtime image sources"
#MISE dir="{{ config_root }}"
#MISE hide=true

set -euo pipefail

git rev-parse HEAD:agents/ate-runtime HEAD:server/cmd/kit-ate-init | git hash-object --stdin | cut -c1-12
