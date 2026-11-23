#!/usr/bin/env bash

#MISE description="Test gateway code mode through the real Rust/Monty runtime"
#MISE dir="{{ config_root }}"

#USAGE flag "--monty-bin <path>" env="GRAM_MONTY_TEST_BIN" help="Monty executable; defaults to the pinned speakeasy-api/codemode release"
#USAGE arg "[args]..." help="Additional go test arguments"

set -euo pipefail
# mise puts the pinned release's executables on PATH. Set
# GRAM_CODE_RUNNER_TEST_BIN and --monty-bin to test a local
# speakeasy-api/codemode build instead.
export GRAM_CODE_RUNNER_TEST_BIN="${GRAM_CODE_RUNNER_TEST_BIN:-$(command -v gram-code-runner || true)}"
export GRAM_MONTY_TEST_BIN="${usage_monty_bin:-$(command -v monty || true)}"
if [ "${1:-}" = "--monty-bin" ]; then
  shift 2
elif [[ "${1:-}" == --monty-bin=* ]]; then
  shift
fi
test -x "$GRAM_CODE_RUNNER_TEST_BIN" || { echo "Run mise install to download the pinned Code Mode runtime" >&2; exit 1; }
test -x "$GRAM_MONTY_TEST_BIN" || { echo "Run mise install or supply --monty-bin" >&2; exit 1; }
exec mise run test:server -tags=codemodeintegration ./internal/codemode/ ./internal/mcp/ ./cmd/gram/ -run Code "$@"
