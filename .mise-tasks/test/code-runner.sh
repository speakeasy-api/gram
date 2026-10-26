#!/usr/bin/env bash

#MISE description="Test the code runner against the real OSS Monty binary"

#USAGE flag "--monty-bin <path>" env="GRAM_MONTY_TEST_BIN" help="Existing executable built from the pinned OSS Monty revision; a relative path resolves from the repository root"
#USAGE arg "[args]..." help="Arguments forwarded to cargo test"

set -euo pipefail
monty_bin="${usage_monty_bin:-${GRAM_MONTY_TEST_BIN:-}}"
if [ -n "$monty_bin" ]; then
  case "$monty_bin" in
    /*) ;;
    *) monty_bin="$MISE_PROJECT_ROOT/$monty_bin" ;;
  esac
  export GRAM_MONTY_TEST_BIN="$monty_bin"
fi
if [ "${1:-}" = "--monty-bin" ]; then
  shift 2
elif [[ "${1:-}" == --monty-bin=* ]]; then
  shift
fi
cd "$MISE_PROJECT_ROOT/agents/code-runner"
exec cargo test --locked "$@"
