#!/usr/bin/env bash

#MISE description="Run the judged team chat memory eval. It spends LLM calls on OPENROUTER_DEV_KEY."
#MISE dir="{{ config_root }}/server"

#USAGE flag "--runs <n>" env="MEMORYEVAL_RUNS" default="3" help="Runs per arm"
#USAGE flag "--models <list>" env="MEMORYEVAL_ANSWER_MODELS" help="Comma separated answering models to compare (default anthropic/claude-sonnet-5.5)"
#USAGE flag "--summary-model <model>" env="MEMORYEVAL_SUMMARY_MODEL" help="Model that writes chat summaries before the memory arms run (default: the live summary model)"
#USAGE flag "--digest-model <model>" env="MEMORYEVAL_DIGEST_MODEL" help="Model that writes day, week and month digests (default: the live digest model)"
#USAGE flag "--out <file>" env="MEMORYEVAL_OUT" help="Also write the markdown report to this file"

set -euo pipefail

if [ -z "${OPENROUTER_DEV_KEY:-}" ] || [ "${OPENROUTER_DEV_KEY}" = "unset" ]; then
  echo "Set OPENROUTER_DEV_KEY in mise.local.toml (mise run zero:openrouter)" >&2
  exit 1
fi

export MEMORYEVAL_RUNS="${usage_runs:-${MEMORYEVAL_RUNS:-3}}"
if [ -n "${usage_models:-}" ]; then
  export MEMORYEVAL_ANSWER_MODELS="$usage_models"
fi
if [ -n "${usage_summary_model:-}" ]; then
  export MEMORYEVAL_SUMMARY_MODEL="$usage_summary_model"
fi
if [ -n "${usage_digest_model:-}" ]; then
  export MEMORYEVAL_DIGEST_MODEL="$usage_digest_model"
fi
if [ -n "${usage_out:-}" ]; then
  out_dir="$(dirname "$usage_out")"
  if [ ! -d "$out_dir" ]; then
    echo "--out directory does not exist: $out_dir" >&2
    exit 1
  fi
  export MEMORYEVAL_OUT="$(cd "$out_dir" && pwd)/$(basename "$usage_out")"
fi

# Every answer and judge call has its own timeout; the overall timeout is only
# a backstop against a hung setup step, sized well above a full four-arm run.
exec mise run test:server -tags=memoryeval -run "^TestMemoryEvalJudged\$" -count=1 -timeout=6h -v ./internal/platformmcp/memoryeval/
