#!/usr/bin/env bash

#MISE description="Apply the shared demo org seed exactly as production does (no local fixtures)"
#MISE dir="{{ config_root }}"

set -euo pipefail

# The production tenant, for verifying the demo org itself against the
# seed/demo/verify.md playbook. For ordinary local development use `mise run
# seed`, which seeds the same data into your own writable org instead.
#
# The SQL is go:embedded, so this is the same code path the daily production
# CronJob runs; the seed's own pre/postflight asserts fail the command on any
# isolation violation.
cd server
exec go run . demo-seed "$@"
