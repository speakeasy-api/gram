#!/usr/bin/env bash

#MISE description="Benchmark prompt-injection detection on the labelled corpus, optionally against another commit, with a viewer"

#USAGE flag "--resume" help="Continue the latest run of this code instead of starting a new one"
#USAGE flag "--compare <ref>" help="Also score this ref, such as main, reusing its latest run, and compare the two case by case"
#USAGE flag "--watch" help="Serve a live viewer while the run progresses"
#USAGE flag "--view" help="Open the viewer for cached results without running"
#USAGE flag "--summary-md" help="Print the summary table as Markdown from cached results without running"

# Runs the detector this change ships on the labelled corpus and fails unless
# it has no false positives and catches at least 80% of all attacks. A full run
# costs about $2.30 in OpenRouter calls.
#
# Every run is new. --resume continues the latest run of the same code instead,
# such as after running out of credit. --compare <ref> also scores that ref
# from a detached worktree, on this branch's fixtures, and compares the two
# case by case; it reuses the ref's latest run, so each compared commit is
# paid for once. Runs are kept under ~/.cache/gram-pi-eval/runs, and the
# viewer lists every one.
#
# Every commit's build is called the same way: from its own checkout, with at
# most -resume and -corpus-dir. Keep it that way, so this script can still run
# old commits.

set -euo pipefail

root=$(git rev-parse --show-toplevel)
cd "$root"
cache="$HOME/.cache/gram-pi-eval"
corpus="$root/server/internal/scanners/promptinjection/testdata/prompt_injection"

resume="${usage_resume:-false}"
compare_ref="${usage_compare:-}"
watch="${usage_watch:-false}"
view_only="${usage_view:-false}"
summary_md="${usage_summary_md:-false}"

bin=$(mktemp -d)
viewer_pid=""
base_pid=""
trap 'for pid in $viewer_pid $base_pid; do kill "$pid" 2>/dev/null || true; done; rm -rf "$bin"' EXIT

build() {
  (cd "$1" && go build -trimpath -buildvcs=false -o "$2" ./server/cmd/risk-pi-report)
}

build "$root" "$bin/change"

# --compare main means origin's main after a fetch, so a comparison uses the
# latest main. A ref origin does not have, such as a commit hash, is used as
# given.
view_args=(view)
compare_sha=""
if [ -n "$compare_ref" ]; then
  git fetch -q origin "$compare_ref" 2>/dev/null || true
  compare_rev="$compare_ref"
  if git rev-parse --verify --quiet "origin/$compare_ref^{commit}" >/dev/null; then
    compare_rev="origin/$compare_ref"
  fi
  compare_sha=$(git rev-parse "$compare_rev^{commit}")
  view_args+=(-base "$compare_sha")
fi

if [ "$summary_md" = "true" ]; then
  exec "$bin/change" "${view_args[@]}" -summary-md
fi
if [ "$view_only" = "true" ]; then
  exec "$bin/change" "${view_args[@]}" -open
fi

base_tree="$cache/worktrees/base"
if [ -n "$compare_sha" ]; then
  compare_short=$(git rev-parse --short=10 "$compare_sha")
  if [ ! -d "$base_tree" ]; then
    git worktree prune
    git worktree add -q --detach "$base_tree" "$compare_sha"
  fi
  git -C "$base_tree" checkout -q --detach "$compare_sha"
  # A build that can -resume keeps per-case records and can reuse them.
  if ! grep -qs '"resume"' "$base_tree/server/cmd/risk-pi-report/main.go"; then
    echo "$compare_rev @ $compare_short predates per-case benchmark records; compare with a newer ref" >&2
    exit 1
  fi
  build "$base_tree" "$bin/base"
fi

if [ "$watch" = "true" ]; then
  "$bin/change" "${view_args[@]}" -serve 127.0.0.1:0 -open &
  viewer_pid=$!
fi

if [ -n "$compare_sha" ]; then
  # The compared commit reuses its latest run, finishing it if it stopped
  # early, so each compared commit is paid for once.
  (cd "$base_tree" && exec "$bin/base" -resume -corpus-dir "$corpus") &
  base_pid=$!
fi
change_args=()
if [ "$resume" = "true" ]; then
  change_args+=(-resume)
fi
change_status=0
"$bin/change" ${change_args[@]+"${change_args[@]}"} || change_status=$?
base_status=0
if [ -n "$base_pid" ]; then
  wait "$base_pid" || base_status=$?
  base_pid=""
fi
if [ "$base_status" -ne 0 ]; then
  echo "warning: the run of $compare_ref did not finish (exit $base_status); rerun to complete it" >&2
fi

# The viewer prints the summary and applies the merge gate to this change.
view_status=0
"$bin/change" "${view_args[@]}" || view_status=$?
if [ -n "$viewer_pid" ]; then
  echo "run finished; the live viewer stays up until Ctrl-C" >&2
  wait "$viewer_pid" || true
fi
if [ "$change_status" -ne 0 ]; then
  exit "$change_status"
fi
exit "$view_status"
