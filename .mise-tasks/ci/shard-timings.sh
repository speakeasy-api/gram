#!/usr/bin/env bash

#MISE description="Refresh ci/cmd/shard/timings.txt from the server-test JUnit reports of recent successful main-branch CI runs"
#MISE dir="{{ config_root }}"

#USAGE flag "--runs <runs>" default="10" help="Number of runs with server-test timing artifacts to read"
#USAGE flag "--branch <branch>" default="main" help="Branch whose pr.yaml push runs are read"
#USAGE flag "--out <file>" default="ci/cmd/shard/timings.txt" help="Timings file to write"

set -euo pipefail

runs="${usage_runs:-10}"
branch="${usage_branch:-main}"
out="${usage_out:-ci/cmd/shard/timings.txt}"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

# Artifacts expire, and runs from before the upload step existed have none, so
# look further back than --runs and stop once enough runs have reports.
candidates=$(gh run list \
  --workflow pr.yaml \
  --branch "$branch" \
  --event push \
  --status success \
  --limit $((runs * 3)) \
  --json databaseId \
  --jq '.[].databaseId')

found=0
for run_id in $candidates; do
  if [ "$found" -ge "$runs" ]; then
    break
  fi

  if gh run download "$run_id" --pattern 'server-test-timings-*' --dir "$workdir/$run_id" >/dev/null 2>&1; then
    found=$((found + 1))
    echo "Downloaded server-test timings from run $run_id"
  else
    echo "Run $run_id has no server-test timing artifacts; skipping"
  fi
done

if [ "$found" -eq 0 ]; then
  echo "No runs with server-test-timings-* artifacts found on $branch." >&2
  exit 1
fi

go run github.com/speakeasy-api/gram/ci/cmd/shard timings -o "$out" "$workdir"
echo "Wrote $out from $found runs. Review the diff and commit it."
