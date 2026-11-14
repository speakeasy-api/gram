#!/usr/bin/env bash

#MISE description="Run auth mock units, then every browser file with preserved evidence"
#MISE dir="{{ config_root }}"

set -euo pipefail
umask 077
output=".logs/mcp-auth/suite-$(date -u +%Y%m%dT%H%M%SZ)-$$"
mkdir -p "$output"
printf 'Evidence directory: %s\n' "$output"

# Invalid protocol fixtures must be fixed before product outcomes are measured.
if mise run test:mcp-auth:unit > "$output/unit.log" 2>&1; then
  printf 'unit\t0\n' > "$output/status.tsv"
else
  code=$?
  printf 'unit\t%s\n' "$code" > "$output/status.tsv"
  printf 'Fixture unit checks failed; browser measurement not started. See %s/unit.log\n' "$output"
  exit "$code"
fi

result=0
# One host and one shared project span every spec file.
export MCP_AUTH_RUN_DIR="$(pwd -P)/$output/run"
export MCP_AUTH_KEEP_SHARED_PROJECT=1
mkdir -p "$MCP_AUTH_RUN_DIR"
node_bin="$(mise which node)"
"$node_bin" e2e/mcp-auth/mock-host.ts "$MCP_AUTH_RUN_DIR" > "$output/mock-host.log" 2>&1 &
mock_host=$!
cleanup() {
  kill "$mock_host" 2>/dev/null || true
  wait "$mock_host" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
for _ in $(seq 1 600); do
  [ -f "$MCP_AUTH_RUN_DIR/host.json" ] && break
  kill -0 "$mock_host" 2>/dev/null || break
  sleep 0.05
done
if [ ! -f "$MCP_AUTH_RUN_DIR/host.json" ]; then
  printf 'Mock host did not start. See %s/mock-host.log\n' "$output"
  exit 1
fi
printf 'Project mode: %s\n' "${MCP_AUTH_PROJECT_MODE:-isolated}"
# A frozen container VM fails everything after it for reasons unrelated to auth.
database_ready() {
  mise exec -- bash -c 'pg_isready -q -d "${GRAM_DATABASE_URL%%\?*}" -t 5'
}
# Spec files run in parallel; organization-wide scenarios then run alone.
for phase in scenarios org-wide-scenarios; do
  if ! database_ready; then
    printf '%s\tdatabase-unreachable\n' "$phase" >> "$output/status.tsv"
    printf 'Database unreachable before %s; results after this point are invalid.\n' "$phase"
    result=1
    break
  fi
  workers=1
  [ "$phase" = scenarios ] && workers="${MCP_AUTH_WORKERS:-3}"
  phase_args=(--project="$phase" --global-timeout=14400000)
  # The regular project succeeded in the previous phase.
  [ "$phase" = org-wide-scenarios ] && phase_args+=(--no-deps)
  if MCP_AUTH_WORKERS="$workers" mise run test:mcp-auth "${phase_args[@]}" > "$output/$phase.log" 2>&1; then
    code=0
  else
    code=$?
    result=1
  fi
  printf '%s\t%s\n' "$phase" "$code" >> "$output/status.tsv"
  if [ -d e2e/.playwright/mcp-auth ]; then
    cp -R e2e/.playwright/mcp-auth "$output/$phase"
  fi
  printf '%s: exit %s\n' "$phase" "$code"
  if [ "$phase" = scenarios ] && [ "$code" -ne 0 ]; then
    printf 'org-wide-scenarios\tskipped-prerequisite-failed\n' >> "$output/status.tsv"
    printf 'Skipping organization-wide measurement because its prerequisite phase failed.\n'
    break
  fi
done
if MCP_AUTH_KEEP_SHARED_PROJECT=0 mise run test:mcp-auth --project=shared-project-cleanup > "$output/shared-project-cleanup.log" 2>&1; then
  code=0
else
  code=$?
  result=1
fi
printf 'shared-project-cleanup\t%s\n' "$code" >> "$output/status.tsv"
printf 'All family attempts finished; results: %s/status.tsv\n' "$output"
exit "$result"
