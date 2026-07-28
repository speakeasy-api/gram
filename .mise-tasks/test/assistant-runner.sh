#!/usr/bin/env bash

#MISE description="Test the assistant runner Rust crate"
#MISE dir="{{ config_root }}/agents/runner"

set -euo pipefail

exec cargo test --locked "$@"
