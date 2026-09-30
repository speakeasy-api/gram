#!/usr/bin/env bash
#MISE description="Build the dedicated OSS Monty runtime image locally"
#MISE dir="{{ config_root }}"
set -euo pipefail
exec docker build --file agents/code-runner/Dockerfile --tag gram-code-runner:test .
