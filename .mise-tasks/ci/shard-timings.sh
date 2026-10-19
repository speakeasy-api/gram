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

# Only runs inside the artifact retention window (retention-days on the upload
# step in pr.yaml) can still have reports, so that bounds how far back to look.
retention_days=30
since=$(date -u -d "${retention_days} days ago" +%F 2>/dev/null || date -u -v-"${retention_days}"d +%F)

# Newest first. Runs from before the upload step existed have no reports and
# are skipped below. Query values go through -f so gh encodes them; a branch
# name can contain characters such as & that would break an interpolated URL.
candidates=$(gh api --method GET --paginate "repos/{owner}/{repo}/actions/workflows/pr.yaml/runs" \
  -f branch="$branch" -f event=push -f status=success -f created=">=${since}" -f per_page=100 \
  --jq '.workflow_runs[].id')

found=0
for run_id in $candidates; do
  if [ "$found" -ge "$runs" ]; then
    break
  fi

  # Listing first separates "this run has no reports" from a failed download,
  # which aborts the refresh rather than silently shrinking the sample.
  available=$(gh api --method GET --paginate "repos/{owner}/{repo}/actions/runs/${run_id}/artifacts" \
    -f per_page=100 --jq '[.artifacts[] | select(.name | startswith("server-test-timings-")) | select(.expired | not)] | length' |
    awk '{ total += $1 } END { print total + 0 }')
  if [ "$available" -eq 0 ]; then
    echo "Run $run_id has no server-test timing artifacts; skipping"
    continue
  fi

  gh run download "$run_id" --pattern 'server-test-timings-*' --dir "$workdir/$run_id"
  found=$((found + 1))
  echo "Downloaded $available server-test timing artifacts from run $run_id"
done

if [ "$found" -eq 0 ]; then
  echo "No runs on $branch since $since have server-test-timings-* artifacts." >&2
  exit 1
fi

if [ "$found" -lt "$runs" ]; then
  echo "Warning: only $found of the requested $runs runs since $since have server-test timing artifacts." >&2
fi

go run github.com/speakeasy-api/gram/ci/cmd/shard timings -o "$out" "$workdir"
echo "Wrote $out from $found runs. Review the diff and commit it."
