#!/usr/bin/env bash

#MISE description="Run auth mock units, then every browser file serially with preserved evidence"
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
# Per-file invocation prevents one family exhausting the whole matrix budget.
# No retries: a failed family remains failed while later families run.
for spec in e2e/mcp-auth/*.spec.ts; do
  name="$(basename "$spec" .spec.ts)"
  if mise run test:mcp-auth "$(basename "$spec")" --global-timeout=1800000 > "$output/$name.log" 2>&1; then
    code=0
  else
    code=$?
    result=1
  fi
  printf '%s\t%s\n' "$name" "$code" >> "$output/status.tsv"
  if [ -d e2e/.playwright/mcp-auth ]; then
    cp -R e2e/.playwright/mcp-auth "$output/$name"
  fi
  printf '%s: exit %s\n' "$name" "$code"
done
printf 'All family attempts finished; results: %s/status.tsv\n' "$output"
exit "$result"
