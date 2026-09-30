#!/usr/bin/env bash

#MISE description="Test gateway code mode through the real Rust/Monty runtime"
#MISE depends=["build:code-runner"]
#MISE dir="{{ config_root }}"

#USAGE flag "--monty-bin <path>" env="GRAM_MONTY_TEST_BIN" help="Executable built from the pinned OSS Monty revision"
#USAGE arg "[args]..." help="Additional go test arguments"

set -euo pipefail
export GRAM_CODE_RUNNER_TEST_BIN="$MISE_PROJECT_ROOT/agents/code-runner/target/debug/gram-code-runner"
export GRAM_MONTY_TEST_BIN="${usage_monty_bin:-$MISE_PROJECT_ROOT/agents/code-runner/target/monty/bin/monty}"
if [ "${1:-}" = "--monty-bin" ]; then
  shift 2
elif [[ "${1:-}" == --monty-bin=* ]]; then
  shift
fi
test -x "$GRAM_MONTY_TEST_BIN" || { echo "Run mise run build:monty or supply --monty-bin" >&2; exit 1; }
exec mise run test:server -tags=codemodeintegration ./internal/codemode/ ./internal/mcp/ -run Code "$@"
