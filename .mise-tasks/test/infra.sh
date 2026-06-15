#!/usr/bin/env bash

#MISE description="Type-check and test the infra Python package (gram_infra)"
#MISE dir="{{ config_root }}/infra"

set -euo pipefail

# --locked: fail loudly when pyproject.toml and uv.lock have drifted instead of
# silently re-resolving. The dev dependency group (pytest, type checkers, trio)
# is installed automatically by uv.
uv sync --locked

uv run pyrefly check --summarize-errors --min-severity warn
uv run ty check
uv run pytest "$@"
