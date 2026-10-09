#!/usr/bin/env bash

#MISE description="Benchmark prompt-injection detection on the labelled corpus: main vs this change, with a viewer"

#USAGE flag "--base <ref>" help="Compare with this ref instead of the merge-base with origin/main"
#USAGE flag "--no-main" help="Run only this change"
#USAGE flag "--watch" help="Serve a live viewer while the run progresses"
#USAGE flag "--view" help="Open the viewer for cached results without running"
#USAGE flag "--summary-md" help="Print the summary table as Markdown from cached results without running"

# Runs the detector each commit ships on the labelled corpus, for this change
# and for main (the merge-base with origin/main), side by side. A full run
# costs about $2.30 per side in OpenRouter calls and fails unless this change
# has no false positives and catches at least 80% of all attacks.
#
# Each side runs its own commit's build of risk-pi-report, which keeps one
# record per case under ~/.cache/gram-pi-eval/runs, keyed by the commit's code
# outside the fixtures. Rerunning resumes, runs only new or edited fixtures,
# and redoes cases that ran out of credit. Main runs from a detached worktree
# of its commit, scored on this branch's fixtures. The viewer lists every
# cached run, so earlier commits stay comparable.
#
# Every commit's build is called the same way: from its own checkout, with at
# most -corpus-dir. Keep it that way, so this script can still run old commits.

set -euo pipefail

root=$(git rev-parse --show-toplevel)
cd "$root"
cache="$HOME/.cache/gram-pi-eval"
corpus="$root/server/internal/scanners/promptinjection/testdata/prompt_injection"

base_ref="${usage_base:-}"
no_main="${usage_no_main:-false}"
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

# Main: the merge-base with origin/main, or --base.
view_args=(view)
base_sha=""
if [ "$no_main" != "true" ]; then
  if [ -z "$base_ref" ]; then
    if ! git fetch -q origin main; then
      if ! git rev-parse --verify --quiet 'origin/main^{commit}' >/dev/null; then
        echo "cannot fetch origin/main and no local copy exists; fetch it, choose --base <ref>, or explicitly use --no-main" >&2
        exit 1
      fi
      echo "warning: could not fetch origin/main; using the existing local origin/main" >&2
    fi
    base_ref=origin/main
    base_sha=$(git merge-base HEAD "$base_ref")
  else
    base_sha=$(git rev-parse "$base_ref^{commit}")
  fi
  view_args+=(-base "$base_sha")
fi

if [ "$summary_md" = "true" ]; then
  exec "$bin/change" "${view_args[@]}" -summary-md
fi
if [ "$view_only" = "true" ]; then
  exec "$bin/change" "${view_args[@]}" -open
fi

base_tree="$cache/worktrees/base"
if [ -n "$base_sha" ]; then
  base_short=$(git rev-parse --short=10 "$base_sha")
  if [ ! -d "$base_tree" ]; then
    git worktree prune
    git worktree add -q --detach "$base_tree" "$base_sha"
  fi
  git -C "$base_tree" checkout -q --detach "$base_sha"
  # measuredCode marks a build that keeps per-case records under the cache.
  if ! grep -qs 'func measuredCode' "$base_tree/server/cmd/risk-pi-report/code.go"; then
    echo "$base_ref @ $base_short predates per-case benchmark records; choose a newer --base <ref>, or explicitly use --no-main" >&2
    exit 1
  fi
  build "$base_tree" "$bin/base"
fi

if [ "$watch" = "true" ]; then
  "$bin/change" "${view_args[@]}" -serve 127.0.0.1:0 -open &
  viewer_pid=$!
fi

# Both sides run at once. When main has the same code as this change, they
# share a run directory, and its lock makes one side wait and reuse the
# other's records.
if [ -n "$base_sha" ]; then
  (cd "$base_tree" && exec "$bin/base" -corpus-dir "$corpus") &
  base_pid=$!
fi
change_status=0
"$bin/change" || change_status=$?
base_status=0
if [ -n "$base_pid" ]; then
  wait "$base_pid" || base_status=$?
  base_pid=""
fi
if [ "$base_status" -ne 0 ]; then
  echo "warning: main's run did not finish (exit $base_status); rerun to complete it" >&2
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
