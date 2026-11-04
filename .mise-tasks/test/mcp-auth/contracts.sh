#!/usr/bin/env bash

#MISE description="Run in-memory product auth-path contracts separately from browser evidence"
#MISE dir="{{ config_root }}"

set -euo pipefail
exec mise run test:server \
  ./internal/mcp ./internal/remotesessions ./internal/remotesessions/delegation \
  -run '^TestAuth(GapPaths|OperationalGap)' -count=1 -p=1 "$@"
