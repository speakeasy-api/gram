#!/usr/bin/env bash

#MISE description="Run the judged team chat memory eval. It spends LLM calls on OPENROUTER_DEV_KEY."
#MISE dir="{{ config_root }}/server"

#USAGE flag "--runs <n>" env="MEMORYEVAL_RUNS" default="3" help="Runs per arm"
#USAGE flag "--out <file>" env="MEMORYEVAL_OUT" help="Also write the markdown report to this file"

set -euo pipefail

if [ -z "${OPENROUTER_DEV_KEY:-}" ] || [ "${OPENROUTER_DEV_KEY}" = "unset" ]; then
  echo "Set OPENROUTER_DEV_KEY in mise.local.toml (mise run zero:openrouter)" >&2
  exit 1
fi

export MEMORYEVAL_RUNS="${usage_runs:-${MEMORYEVAL_RUNS:-3}}"
if [ -n "${usage_out:-}" ]; then
  export MEMORYEVAL_OUT="$(cd "$(dirname "$usage_out")" && pwd)/$(basename "$usage_out")"
fi

exec mise run test:server -tags=memoryeval -run "^TestMemoryEvalJudged\$" -count=1 -timeout=60m -v ./internal/platformmcp/memoryeval/
