#!/usr/bin/env bash

#MISE description="Test the code runner against the real OSS Monty binary"
#MISE dir="{{ config_root }}/agents/code-runner"

#USAGE flag "--monty-bin <path>" env="GRAM_MONTY_TEST_BIN" help="Existing executable built from the pinned OSS Monty revision"
#USAGE arg "[args]..." help="Arguments forwarded to cargo test"

set -euo pipefail
if [ -n "${usage_monty_bin:-}" ]; then
  export GRAM_MONTY_TEST_BIN="$usage_monty_bin"
fi
if [ "${1:-}" = "--monty-bin" ]; then
  shift 2
elif [[ "${1:-}" == --monty-bin=* ]]; then
  shift
fi
exec cargo test --locked "$@"
