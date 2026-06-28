#!/usr/bin/env bash

#MISE description="Type-check and test the infra Python package (gram_infra)"
#MISE dir="{{ config_root }}/infra"

set -euo pipefail

uv run --no-sync pyrefly check --summarize-errors --min-severity warn
uv run --no-sync ty check
uv run --no-sync pytest "$@"
gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- -race ./...