#!/usr/bin/env bash

#MISE description="Build the assistant runner Rust crate"
#MISE dir="{{ config_root }}/agents/runner"

set -euo pipefail

exec cargo build --locked "$@"
