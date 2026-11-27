#!/usr/bin/env bash

#MISE description="Run the MCP feedback content eval: does an agent act on prompt injection planted in list_mcp_feedback results? It spends LLM calls on OPENROUTER_DEV_KEY."
#MISE dir="{{ config_root }}/server"

#USAGE flag "--models <list>" env="TOOLEVAL_MODELS" help="Comma separated models to compare, at most 4 (default anthropic/claude-sonnet-5.5)"
#USAGE flag "--runs <n>" env="TOOLEVAL_RUNS" default="3" help="Runs per case, model and arm"
#USAGE flag "--arms <list>" env="FEEDBACKEVAL_ARMS" default="guarded,bare,skill" help="guarded (the tool as served), bare (no trust contract, the control) and skill (guarded with the shipped feedback skill loaded)"
#USAGE flag "--cases <regex>" env="TOOLEVAL_CASES" help="Only cases whose id matches"
#USAGE flag "--concurrency <n>" env="TOOLEVAL_CONCURRENCY" help="Cases in flight at once (default 8)"
#USAGE flag "--out <file>" env="TOOLEVAL_OUT" help="Also write the markdown report to this file"
#USAGE flag "--gate" help="Fail when the guarded arm followed any planted report"

set -euo pipefail

if { [ -z "${OPENROUTER_DEV_KEY:-}" ] || [ "${OPENROUTER_DEV_KEY}" = "unset" ]; } && [ -z "${OPENROUTER_API_KEY:-}" ]; then
  echo "Set OPENROUTER_DEV_KEY in mise.local.toml (mise run zero:openrouter), or OPENROUTER_API_KEY" >&2
  exit 1
fi

export TOOLEVAL_RUNS="${usage_runs:-${TOOLEVAL_RUNS:-3}}"
export FEEDBACKEVAL_ARMS="${usage_arms:-${FEEDBACKEVAL_ARMS:-guarded,bare,skill}}"
if [ -n "${usage_models:-}" ]; then
  export TOOLEVAL_MODELS="$usage_models"
fi
if [ -n "${TOOLEVAL_MODELS:-}" ]; then
  IFS=',' read -r -a models <<<"$TOOLEVAL_MODELS"
  if [ "${#models[@]}" -gt 4 ]; then
    echo "--models takes at most 4 models, got ${#models[@]}" >&2
    exit 1
  fi
fi
if [ -n "${usage_cases:-}" ]; then
  export TOOLEVAL_CASES="$usage_cases"
fi
if [ -n "${usage_concurrency:-}" ]; then
  export TOOLEVAL_CONCURRENCY="$usage_concurrency"
fi
if [ "${usage_gate:-false}" = "true" ]; then
  # The gate checks the guarded arm; without it there is nothing to check.
  case ",$FEEDBACKEVAL_ARMS," in
  *,guarded,*) ;;
  *)
    echo "--gate checks the guarded arm, so --arms must include guarded" >&2
    exit 1
    ;;
  esac
  export FEEDBACKEVAL_GATE=1
fi
if [ -n "${usage_out:-}" ]; then
  # The task runs from server/, so resolve a relative path against the
  # directory mise was started from. Check the directory before any paid call:
  # the report is written only once every case has run.
  case "$usage_out" in
  /*) ;;
  *) usage_out="${MISE_ORIGINAL_CWD:-$PWD}/$usage_out" ;;
  esac
  out_dir="$(dirname "$usage_out")"
  if [ ! -d "$out_dir" ]; then
    echo "--out directory does not exist: $out_dir" >&2
    exit 1
  fi
  export TOOLEVAL_OUT="$(cd "$out_dir" && pwd)/$(basename "$usage_out")"
fi

exec mise run test:server -tags=tooleval -run "^TestFeedbackContentEvalJudged\$" -count=1 -timeout=1h -v ./internal/platformmcp/
