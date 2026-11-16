#!/usr/bin/env bash

#MISE description="Build the pinned OSS Monty subprocess runtime"
#MISE dir="{{ config_root }}/agents/code-runner"

set -euo pipefail
exec cargo install --locked --no-default-features --git https://github.com/pydantic/monty \
  --rev 15753b35e8eee6d569f223cd19f0303dbf07f896 \
  --root target/monty --bin monty monty-runtime
