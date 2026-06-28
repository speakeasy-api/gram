#!/usr/bin/env bash

#MISE description="Run linters against pystreams package"
#MISE dir="{{ config_root }}/pystreams"

set -eo pipefail

gum log --level info "Running ty"
uv run --no-sync ty check

echo ""

gum log --level info "Running pyrefly"
uv run --no-sync pyrefly check --summarize-errors --min-severity warn

echo ""

gum log --level info "Running ruff"
ruff check