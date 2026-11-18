#!/usr/bin/env bash

#MISE description="Run the Platform MCP tool-selection eval. It spends LLM calls on OPENROUTER_DEV_KEY."
#MISE dir="{{ config_root }}/server"

#USAGE flag "--models <list>" env="TOOLEVAL_MODELS" help="Comma separated models to compare, at most 4 (default anthropic/claude-sonnet-5.5)"
#USAGE flag "--runs <n>" env="TOOLEVAL_RUNS" default="1" help="Runs per case, model and surface"
#USAGE flag "--surfaces <list>" env="TOOLEVAL_SURFACES" default="direct,progressive" help="Discovery surfaces: direct (full catalogue), progressive (search, describe and call)"
#USAGE flag "--cases <regex>" env="TOOLEVAL_CASES" help="Only cases whose id or file name matches"
#USAGE flag "--changed-since <ref>" help="Only run cases a change since this ref could move, compared at the merge base (e.g. origin/main)"
#USAGE flag "--max-cases <n>" env="TOOLEVAL_MAX_CASES" help="Run at most this many cases, sampled stably by case id"
#USAGE flag "--concurrency <n>" env="TOOLEVAL_CONCURRENCY" help="Cases in flight at once (default 8); lower it if the key is rate limited"
#USAGE flag "--out <file>" env="TOOLEVAL_OUT" help="Also write the markdown report to this file"
#USAGE flag "--gate" help="Fail when a case that passes in tooleval/baseline.json now fails in most runs"
#USAGE flag "--baseline-out <file>" env="TOOLEVAL_BASELINE_OUT" help="Write tooleval/baseline.json updated with this run's results to this file"

set -euo pipefail

# CI passes OPENROUTER_API_KEY, because mise.toml sets OPENROUTER_DEV_KEY to
# an "unset" placeholder that would override a key from the environment.
if { [ -z "${OPENROUTER_DEV_KEY:-}" ] || [ "${OPENROUTER_DEV_KEY}" = "unset" ]; } && [ -z "${OPENROUTER_API_KEY:-}" ]; then
  echo "Set OPENROUTER_DEV_KEY in mise.local.toml (mise run zero:openrouter), or OPENROUTER_API_KEY" >&2
  exit 1
fi

export TOOLEVAL_RUNS="${usage_runs:-${TOOLEVAL_RUNS:-1}}"
export TOOLEVAL_SURFACES="${usage_surfaces:-${TOOLEVAL_SURFACES:-direct,progressive}}"
if [ -n "${usage_models:-}" ]; then
  export TOOLEVAL_MODELS="$usage_models"
fi
# Spend grows with every model, and the direct surface lists every tool.
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
if [ -n "${usage_max_cases:-}" ]; then
  export TOOLEVAL_MAX_CASES="$usage_max_cases"
fi
if [ -n "${usage_concurrency:-}" ]; then
  export TOOLEVAL_CONCURRENCY="$usage_concurrency"
fi
if [ "${usage_gate:-false}" = "true" ]; then
  export TOOLEVAL_GATE=1
fi
if [ -n "${usage_baseline_out:-}" ]; then
  case "$usage_baseline_out" in
  /*) ;;
  *) usage_baseline_out="${MISE_ORIGINAL_CWD:-$PWD}/$usage_baseline_out" ;;
  esac
  export TOOLEVAL_BASELINE_OUT="$usage_baseline_out"
fi
if [ "${TOOLEVAL_GATE:-}" = "1" ] && [ ! -s internal/platformmcp/tooleval/baseline.json ]; then
  # A gated run with no committed baseline has nothing to compare against,
  # so it runs every case uncapped and --baseline-out writes the first
  # baseline. Report-only runs keep their change selection.
  echo "No committed tool eval baseline, so every case runs to take one" >&2
  usage_changed_since=""
  unset TOOLEVAL_MAX_CASES
fi
if [ -n "${usage_changed_since:-}" ]; then
  # The catalogue snapshot and case files where this branch left the ref. The
  # test compares them with the live catalogue and the current cases.
  tooleval_path="server/internal/platformmcp/tooleval"
  merge_base="$(git merge-base "$usage_changed_since" HEAD)"
  if git cat-file -e "$merge_base:$tooleval_path/catalogue.json" 2>/dev/null; then
    base_dir="$(mktemp -d)"
    trap 'rm -rf "$base_dir"' EXIT
    git show "$merge_base:$tooleval_path/catalogue.json" >"$base_dir/catalogue.json"
    mkdir "$base_dir/cases"
    # The task runs from server/, and git archive names entries relative to
    # the working directory, so archive from the repository root.
    git -C "$(git rev-parse --show-toplevel)" archive "$merge_base" "$tooleval_path/cases" | tar -x -C "$base_dir/cases" --strip-components=5
    export TOOLEVAL_BASE_DIR="$base_dir"
  else
    # The first run, where the base has no catalogue yet, is the baseline:
    # every case, with no cap.
    echo "The merge base with $usage_changed_since has no tool eval catalogue, so every case runs as the baseline" >&2
    unset TOOLEVAL_MAX_CASES
  fi
fi
if [ -n "${usage_out:-}" ]; then
  # The task runs from server/, so resolve a relative path against the
  # directory mise was started from.
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

# Not exec: the EXIT trap removes the base copy once the run ends.
mise run test:server -tags=tooleval -run "^TestToolEvalJudged\$" -count=1 -timeout=3h -v ./internal/platformmcp/
