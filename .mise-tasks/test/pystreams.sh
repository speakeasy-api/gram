#!/usr/bin/env bash

#MISE description="Test the pystreams python package"
#MISE dir="{{ config_root }}/pystreams"

set -euo pipefail

uv run --no-sync pytest "$@"
