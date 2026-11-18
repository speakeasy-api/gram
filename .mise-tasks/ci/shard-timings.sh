#!/usr/bin/env bash

#MISE description="Refresh ci/cmd/shard/timings.txt from the server-test JUnit reports of recent successful merge queue CI runs"
#MISE dir="{{ config_root }}"

#USAGE flag "--runs <runs>" default="10" help="Number of runs with server-test timing artifacts to read"
#USAGE flag "--branch <branch>" default="main" help="Branch whose pr.yaml merge queue runs are read"
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
# are skipped below. Query values go through -f so gh encodes them. Pushes to
# the branch skip server-test because the merge queue already ran it on the
# same commit, so the reports come from the queue's gh-readonly-queue/<branch>/
# runs, which the API can only filter by exact branch name.
list_candidates() {
  local start end count midpoint
  start=$(jq -nr --argjson epoch "$1" '$epoch | todateiso8601') || return
  end=$(jq -nr --argjson epoch "$2" '$epoch | todateiso8601') || return
  count=$(gh api --method GET "repos/{owner}/{repo}/actions/workflows/pr.yaml/runs" \
    -f event=merge_group -f status=success -f created="${start}..${end}" -f per_page=1 \
    --jq '.total_count') || return

  # Filtered workflow queries expose at most 1,000 runs, even with pagination.
  # Split crowded windows before filtering by branch, keeping newest first.
  if [ "$count" -gt 1000 ]; then
    if [ "$1" -eq "$2" ]; then
      echo "More than 1,000 successful merge queue runs at $start; cannot list all runs." >&2
      return 1
    fi
    midpoint=$((($1 + $2) / 2))
    list_candidates "$((midpoint + 1))" "$2" || return
    list_candidates "$1" "$midpoint" || return
  elif [ "$count" -gt 0 ]; then
    QUEUE_PREFIX="gh-readonly-queue/${branch}/" gh api --method GET --paginate "repos/{owner}/{repo}/actions/workflows/pr.yaml/runs" \
      -f event=merge_group -f status=success -f created="${start}..${end}" -f per_page=100 \
      --jq '.workflow_runs[] | select(.head_branch | startswith(env.QUEUE_PREFIX)) | .id' || return
  fi
}

since_epoch=$(jq -nr --arg since "${since}T00:00:00Z" '$since | fromdateiso8601')
candidates=$(list_candidates "$since_epoch" "$(date -u +%s)")

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
