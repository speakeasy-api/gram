#!/usr/bin/env bash

#MISE description="Run rustfmt and Clippy on the assistant runner"
#MISE dir="{{ config_root }}/agents/runner"

set -euo pipefail

cargo fmt --check
cargo clippy --locked --all-targets --all-features -- -D warnings
