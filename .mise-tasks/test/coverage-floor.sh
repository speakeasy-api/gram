#!/usr/bin/env bash

#MISE description="Enforce per-package coverage floors for security/billing/utility packages"
#MISE dir="{{ config_root }}/server"

#USAGE flag "--update" help="Print the floors as they would be after running, useful for raising thresholds"

set -euo pipefail

# Each entry is "<package-path>:<min-percentage>".
# Raise floors over time as test coverage improves; never lower them.
# Run with --update to print suggested floors based on current coverage.
floors=(
  "internal/encryption:75"
  "internal/billing:75"
  "internal/conv:40"
  "internal/cache:40"
  "internal/gateway:55"
)

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

failed=()
results=()

for entry in "${floors[@]}"; do
  pkg="${entry%:*}"
  floor="${entry##*:}"
  cover_file="$tmpdir/$(echo "$pkg" | tr / _).out"

  if ! go test -count=1 -covermode=atomic -coverprofile="$cover_file" "./$pkg" >/dev/null; then
    echo "FAIL: ./$pkg tests failed"
    failed+=("$pkg")
    continue
  fi

  if [ ! -s "$cover_file" ]; then
    echo "FAIL: ./$pkg produced no coverage profile (no test files?)"
    failed+=("$pkg")
    continue
  fi

  pct=$(go tool cover -func="$cover_file" | awk '/^total:/ {gsub("%","",$3); print $3}')
  if [ -z "$pct" ]; then
    echo "FAIL: ./$pkg coverage could not be parsed"
    failed+=("$pkg")
    continue
  fi

  results+=("$pkg:$pct:$floor")

  # Compare floats with awk to avoid bc dependency.
  below=$(awk -v have="$pct" -v want="$floor" 'BEGIN { print (have+0 < want+0) ? "1" : "0" }')
  if [ "$below" = "1" ]; then
    echo "FAIL: ./$pkg coverage $pct% < floor $floor%"
    failed+=("$pkg")
  fi
done

echo
echo "Coverage summary:"
printf '%-45s %8s %8s\n' "package" "actual" "floor"
printf '%-45s %8s %8s\n' "-------" "------" "-----"
for r in "${results[@]}"; do
  pkg="${r%%:*}"
  rest="${r#*:}"
  pct="${rest%:*}"
  floor="${rest##*:}"
  printf '%-45s %7s%% %7s%%\n' "$pkg" "$pct" "$floor"
done

if [ "${usage_update:-false}" = "true" ]; then
  echo
  echo "Suggested floors (set to actual, rounded down):"
  for r in "${results[@]}"; do
    pkg="${r%%:*}"
    rest="${r#*:}"
    pct="${rest%:*}"
    rounded=$(awk -v v="$pct" 'BEGIN { printf "%d", v }')
    echo "  \"$pkg:$rounded\""
  done
fi

if [ ${#failed[@]} -gt 0 ]; then
  echo
  echo "Coverage floor check failed for: ${failed[*]}"
  exit 1
fi

echo
echo "All packages meet their coverage floors."
