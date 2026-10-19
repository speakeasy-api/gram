#!/usr/bin/env bash

#MISE description="Test the server with Bazel. Takes go test style arguments (packages relative to server/, -run, -count, -race, -tags, -p, -timeout, -v, -args) plus --cover, --shard and --rerun-fails."
#MISE dir="{{ config_root }}/server"

# Arguments keep the 'go test' shape so existing docs and habits still work;
# each is translated to its Bazel equivalent below. Anything Bazel-specific can
# be passed after '--bazel', e.g. 'mise run test:server ./internal/oops --bazel --test_output=all'.

set -uo pipefail

cover=false
open_html=false
shard=""
shard_isolate=""
rerun_fails=""
verbose=false
patterns=()
tags=""
tags_given=false
bazel_args=()
test_args=()

die() {
  echo "test:server: $*" >&2
  exit 2
}

# go_duration_seconds turns a Go duration such as 20m, 90s or 1h30m into whole
# seconds for --test_timeout.
go_duration_seconds() {
  local rest=$1 total=0 num unit
  while [ -n "$rest" ]; do
    num=${rest%%[!0-9]*}
    rest=${rest#"$num"}
    unit=${rest%%[0-9]*}
    rest=${rest#"$unit"}
    [ -n "$num" ] || die "unsupported duration: $1"
    case $unit in
      h) total=$((total + num * 3600)) ;;
      m) total=$((total + num * 60)) ;;
      s) total=$((total + num)) ;;
      *) die "unsupported duration: $1" ;;
    esac
  done
  echo "$total"
}

# to_target maps a package pattern as 'go test' takes it from server/ to a Bazel
# target pattern.
to_target() {
  local p=$1
  case $p in
    github.com/speakeasy-api/gram/*) p="//${p#github.com/speakeasy-api/gram/}" ;;
    github.com/speakeasy-api/gram) p="//" ;;
    .|./) p="//server" ;;
    ./*) p="//server/${p#./}" ;;
    *) p="//server/$p" ;;
  esac
  p=${p%/}
  case $p in
    */...) echo "$p" ;;
    *) echo "$p:all" ;;
  esac
}

needs_value() {
  [ $# -ge 2 ] || die "$1 needs a value"
}

while [ $# -gt 0 ]; do
  arg=$1
  shift
  case $arg in
    --cover) cover=true ;;
    --html) open_html=true ;;
    --shard=*) shard="${arg#--shard=}" ;;
    --shard-isolate=*) shard_isolate="${arg#--shard-isolate=}" ;;
    --rerun-fails=*) rerun_fails="${arg#--rerun-fails=}" ;;
    --bazel)
      bazel_args+=("$@")
      break ;;
    -args|--args)
      for a in "$@"; do
        test_args+=("--test_arg=$a")
      done
      break ;;
    -race|--race) bazel_args+=(--config=race) ;;
    -v|--v|-v=true) verbose=true ;;
    -run=*|--run=*) bazel_args+=("--test_filter=${arg#*=}") ;;
    -run|--run)
      needs_value "$arg" "$@"
      bazel_args+=("--test_filter=$1")
      shift ;;
    -count=*|--count=*)
      bazel_args+=(--nocache_test_results)
      count=${arg#*=}
      [ "$count" = 1 ] || test_args+=("--test_arg=-test.count=$count") ;;
    -p=*|--p=*) bazel_args+=("--local_test_jobs=${arg#*=}") ;;
    -p|--p)
      needs_value "$arg" "$@"
      bazel_args+=("--local_test_jobs=$1")
      shift ;;
    -tags=*|--tags=*)
      tags=${arg#*=}
      tags_given=true ;;
    -tags|--tags)
      needs_value "$arg" "$@"
      tags=$1
      tags_given=true
      shift ;;
    -timeout=*|--timeout=*)
      bazel_args+=("--test_timeout=$(go_duration_seconds "${arg#*=}")") ;;
    -timeout|--timeout)
      needs_value "$arg" "$@"
      bazel_args+=("--test_timeout=$(go_duration_seconds "$1")")
      shift ;;
    # Relative paths, anything ending in "..." and import paths.
    ./*|../*|.|*...|*.*/*) patterns+=("$arg") ;;
    # Other test binary flags (-short, -failfast, -shuffle=on, ...) go to every
    # test binary as -test.<flag>.
    -*) test_args+=("--test_arg=-test.${arg#-}") ;;
    *) patterns+=("$arg") ;;
  esac
done

if [ ${#patterns[@]} -eq 0 ]; then
  patterns=("./...")
fi

# Without -tags the .bazelrc default (inv.debug) applies, matching what this
# task has always defaulted to. An explicit -tags replaces it, as it did with
# 'go test'.
if [ "$tags_given" = true ]; then
  bazel_args+=("--@rules_go//go/config:tags=$tags")
fi

# --shard=<index>/<total> runs a deterministic subset of the packages that have
# tests, so CI can spread the suite over several runners. See ci/cmd/shard for
# how packages are distributed. --shard-isolate=<import path>[,...] gives each
# listed package a shard of its own, taking the last shards.
if [ -n "$shard" ]; then
  shard_tags=()
  if [ -n "$tags" ]; then
    shard_tags=("-tags=$tags")
  elif [ "$tags_given" = false ]; then
    shard_tags=("-tags=inv.debug")
  fi
  shard_packages=$(go run -buildvcs=false github.com/speakeasy-api/gram/ci/cmd/shard \
    -i "$shard" -isolate "$shard_isolate" ${shard_tags[@]+"${shard_tags[@]}"} "${patterns[@]}") || exit $?

  patterns=()
  while IFS= read -r line; do
    [ -n "$line" ] && patterns+=("$line")
  done <<< "$shard_packages"

  if [ ${#patterns[@]} -eq 0 ]; then
    echo "Shard $shard: no packages to test."
    exit 0
  fi
fi

targets=()
for p in "${patterns[@]}"; do
  targets+=("$(to_target "$p")")
done

# --rerun-fails=<n> retries a failing test target up to n more times, so a
# flaky test does not fail the run on its own. Tests that only passed on a
# retry are listed in rerun-report.txt; without it a flake is invisible.
if [ -n "$rerun_fails" ]; then
  bazel_args+=("--flaky_test_attempts=$((rerun_fails + 1))")
fi

if [ "$verbose" = true ]; then
  bazel_args+=(--test_output=all --test_arg=-test.v)
else
  # Failures are printed below from the JUnit conversion, gotestsum style.
  bazel_args+=(--test_output=summary)
fi

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT

command=test
if [ "$cover" = true ]; then
  # The coverage settings live in .bazelrc.
  command=coverage
fi

# GO_TEST_WRAP_TESTV makes rules_go log every test with test2json framing, which
# ci/cmd/testlogs needs to rebuild the 'go test -json' stream.
bazel "$command" \
  --build_tests_only \
  --keep_going \
  --test_env=GO_TEST_WRAP_TESTV=1 \
  --build_event_json_file="$scratch/bep.json" \
  ${bazel_args[@]+"${bazel_args[@]}"} \
  ${test_args[@]+"${test_args[@]}"} \
  -- "${targets[@]}"
test_exit_code=$?

# 0: passed, 3: some tests failed, 4: no tests matched. Anything else is a build
# or infrastructure failure with no test logs to report.
case $test_exit_code in
  0|3) ;;
  4)
    echo "No test targets matched: ${targets[*]}"
    exit 0 ;;
  *) exit $test_exit_code ;;
esac

report_flags=()
if [ -n "$rerun_fails" ]; then
  report_flags+=(-rerun-report rerun-report.txt)
fi
if [ "$cover" = true ]; then
  report_flags+=(-coverprofile cover.out)
fi
go run -buildvcs=false github.com/speakeasy-api/gram/ci/cmd/testlogs \
  -bep "$scratch/bep.json" -o "$scratch/events.json" ${report_flags[@]+"${report_flags[@]}"} || exit $?

# gotestsum replays the events: it prints failures the way this task always
# has, writes junit-report.xml for CI, and honours GOTESTSUM_JSONFILE_TIMING_EVENTS.
gotestsum --junitfile junit-report.xml --format-hide-empty-pkg --format pkgname-and-test-fails \
  --raw-command -- cat "$scratch/events.json" || true

if [ "$cover" = true ]; then
  go tool cover -html=cover.out -o cover.html
  echo "Coverage report generated: cover.html"

  if [ "$open_html" = true ]; then
    if command -v open >/dev/null 2>&1; then
      open cover.html
    elif command -v xdg-open >/dev/null 2>&1; then
      xdg-open cover.html
    else
      echo "Could not open browser automatically. Please open cover.html manually."
    fi
  fi
fi

exit $test_exit_code
