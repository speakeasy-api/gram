#!/usr/bin/env bash

#MISE description="Run Go tests in Dagger and export reports even when tests fail"
#MISE dir="{{ config_root }}"

#USAGE flag "--component <component>" required=#true { choices "server" "cli" "plog" "functions" "hooks" "glint" "ci" }
#USAGE flag "--shard <shard>" help="Server test shard, index/total"
#USAGE flag "--race" help="Enable the Go race detector"
#USAGE flag "--demo-seed" help="Run the separately tagged demo seed safety test"
#USAGE flag "--packages <packages>" help="Comma-separated Go package patterns" default="./..."

set -euo pipefail

component=${usage_component:?}
out="$component"
if [ "$component" = ci ]; then out=.; fi
args=(test --component "$component" --packages "${usage_packages:-./...}")
if [ -n "${usage_shard:-}" ]; then args+=(--shard "$usage_shard"); fi
if [ "${usage_race:-false}" = true ]; then args+=(--race); fi
if [ "${usage_demo_seed:-false}" = true ]; then args+=(--demo-seed); fi

# A missing report must never leave results from a previous run.
rm -f "$out/junit-report.xml" "$out/test-timings.json" "$out/test-exit-code" "$out/test-output.txt"
dagger api call go-pipeline "${args[@]}" export --path "$out"
status=$(cat "$out/test-exit-code")
if [[ ! "$status" =~ ^[0-9]+$ ]] || [ "$status" -gt 255 ]; then
  echo "Invalid test exit code: $status" >&2
  exit 1
fi
if [ "$status" -ne 0 ] && [ -f "$out/test-output.txt" ]; then
  cat "$out/test-output.txt" >&2
fi
exit "$status"
