#!/usr/bin/env bash

#MISE description="Benchmark prompt-injection detection on the labelled corpus: main vs this change, with a viewer"

#USAGE flag "--sources <sources>" help="Comma-separated source substrings to run, for a cheap slice; the merge gate needs a full run"
#USAGE flag "--base <ref>" help="Compare with this ref instead of the merge-base with origin/main"
#USAGE flag "--no-main" help="Run only this change"
#USAGE flag "--watch" help="Serve a live viewer while the run progresses"
#USAGE flag "--view" help="Open the viewer for cached results without running"
#USAGE flag "--summary-md" help="Print the summary table as Markdown from cached results without running"

# Runs the production Jev -> confirmer cascade on the labelled corpus for this
# change and for main (the merge-base with origin/main), side by side. A full
# run costs about $2.30 per side in OpenRouter calls and fails unless this
# change has no false positives and catches at least 80% of all attacks.
#
# Results are cached per case under ~/.cache/gram-pi-eval, keyed by the code
# (the commit's tree without the fixtures, plus any uncommitted change) and
# the case's content. Rerunning resumes, runs only new or edited fixtures, and
# redoes cases that ran out of credit once the balance covers them. Main runs
# from a detached worktree of its commit, scored on this branch's fixtures.

set -euo pipefail

root=$(git rev-parse --show-toplevel)
cd "$root"
cache="$HOME/.cache/gram-pi-eval"
fixtures="server/internal/scanners/promptinjection/testdata"
corpus="$root/$fixtures/prompt_injection"
gate_args=(-max-false-positives=0 -min-recall=0.80)

sources="${usage_sources:-}"
base_ref="${usage_base:-}"
no_main="${usage_no_main:-false}"
watch="${usage_watch:-false}"
view_only="${usage_view:-false}"
summary_md="${usage_summary_md:-false}"

# code_key names a commit's code without its fixtures, so a fixture-only
# commit reuses every unedited case's result.
code_key() {
  git ls-tree -r "$1" | grep -vF "$fixtures/" | git hash-object --stdin | cut -c1-12
}

build() {
  (cd "$1" && go build -trimpath -buildvcs=false -o "$2" ./server/cmd/risk-pi-report)
}

mkdir -p "$cache/runs" "$cache/bin"

# This change: HEAD's code plus any uncommitted change outside the fixtures.
head_short=$(git rev-parse --short=10 HEAD)
change_key=$(code_key HEAD)
change_ref="$(git rev-parse --abbrev-ref HEAD) @ $head_short"
if [ -n "$(git status --porcelain -- . ":(exclude)$fixtures")" ]; then
  dirty=$({
    git diff --binary HEAD -- . ":(exclude)$fixtures"
    git ls-files -z --others --exclude-standard -- . ":(exclude)$fixtures" | while IFS= read -r -d '' f; do
      printf '%s\0' "$f"
      git hash-object -- "$f"
    done
  } | git hash-object --stdin | cut -c1-8)
  change_key="$change_key-$dirty"
  change_ref="$change_ref + uncommitted"
fi
change_dir="$cache/runs/$change_key"
change_bin="$cache/bin/risk-pi-$change_key"
build "$root" "$change_bin"

common=(-cascade -corpus-dir "$corpus" -exclude-sources=cascade_context -check-floors=false)
view_args=("${common[@]}" -view -run-dir "$change_dir")
if [ -n "$sources" ]; then
  common+=(-sources "$sources")
  view_args+=(-sources "$sources")
else
  view_args+=("${gate_args[@]}")
fi

# Main: the merge-base with origin/main, or --base, from a detached worktree.
run_main=false
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
  base_short=$(git rev-parse --short=10 "$base_sha")
  base_key=$(code_key "$base_sha")
  base_dir="$cache/runs/$base_key"
  base_tree="$cache/worktrees/base"
  if [ ! -d "$base_tree" ]; then
    git worktree prune
    git worktree add -q --detach "$base_tree" "$base_sha"
  fi
  git -C "$base_tree" checkout -q --detach "$base_sha"
  if grep -q '"run-dir"' "$base_tree/server/cmd/risk-pi-report/main.go"; then
    run_main=true
    view_args+=(-base-run-dir "$base_dir")
  else
    echo "baseline @ $base_short predates per-case benchmark records; choose a compatible --base <ref>, or explicitly use --no-main for a one-sided run" >&2
    exit 1
  fi
fi

if [ "$summary_md" = "true" ]; then
  exec "$change_bin" "${view_args[@]}" -summary-md
fi
if [ "$view_only" = "true" ]; then
  exec "$change_bin" "${view_args[@]}" -open
fi

viewer_pid=""
base_pid=""
trap 'for pid in $viewer_pid $base_pid; do kill "$pid" 2>/dev/null || true; done' EXIT
if [ "$watch" = "true" ]; then
  "$change_bin" "${view_args[@]}" -serve 127.0.0.1:0 -open &
  viewer_pid=$!
fi

change_args=("${common[@]}" -run-dir "$change_dir" -label "this change" -ref "$change_ref")
if [ -z "$sources" ]; then
  change_args+=("${gate_args[@]}")
else
  echo "slice ($sources): the merge gate is not applied; run without --sources before the PR" >&2
fi

base_status=0
if [ "$run_main" = "true" ] && [ "$base_key" = "$change_key" ]; then
  echo "main @ $base_short has the same code as this change, so one run covers both columns" >&2
elif [ "$run_main" = "true" ]; then
  base_bin="$cache/bin/risk-pi-$base_key"
  if [ ! -x "$base_bin" ]; then
    build "$base_tree" "$base_bin"
  fi
  "$base_bin" "${common[@]}" -run-dir "$base_dir" -label "main" -ref "$base_ref @ $base_short" &
  base_pid=$!
fi

change_status=0
"$change_bin" "${change_args[@]}" || change_status=$?
if [ -n "$base_pid" ]; then
  wait "$base_pid" || base_status=$?
  base_pid=""
fi
if [ "$base_status" -ne 0 ]; then
  echo "warning: main's run did not finish (exit $base_status); rerun to complete it" >&2
fi

"$change_bin" "${view_args[@]}"
"$change_bin" "${view_args[@]}" -summary-md
if [ -n "$viewer_pid" ]; then
  echo "run finished; the live viewer stays up until Ctrl-C" >&2
  wait "$viewer_pid" || true
fi
exit "$change_status"
