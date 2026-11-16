#!/usr/bin/env bash

#MISE description="Check code runner formatting and Clippy"
#MISE dir="{{ config_root }}/agents/code-runner"

set -euo pipefail
cargo fmt --check
cargo clippy --locked --all-targets -- -D warnings
