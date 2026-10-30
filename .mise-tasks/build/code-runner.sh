#!/usr/bin/env bash

#MISE description="Build the ephemeral Monty code runner"
#MISE dir="{{ config_root }}/agents/code-runner"

set -euo pipefail
exec cargo build --locked "$@"
