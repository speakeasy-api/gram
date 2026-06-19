#!/usr/bin/env bash

#MISE description="Run linters against pystreams package"
#MISE dir="{{ config_root }}/pystreams"

set -eo pipefail

uv run --no-sync ty check
uv run --no-sync pyrefly check --summarize-errors --min-severity warn