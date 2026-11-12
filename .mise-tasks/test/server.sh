#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Test the server with optional coverage generation. It takes the same arguments as 'go test'."

# Check if flags are provided
cover=false
open_html=false
shard=""
shard_isolate=""
shard_changed=""
plan=""
print_shard=false
rerun_fails=""
args=()

for arg in "$@"; do
  case $arg in
    --cover)
      cover=true
      shift ;;
    --html)
      open_html=true
      shift ;;
    --shard=*)
      shard="${arg#--shard=}"
      shift ;;
    --shard-isolate=*)
      shard_isolate="${arg#--shard-isolate=}"
      shift ;;
    --shard-changed=*)
      shard_changed="${arg#--shard-changed=}"
      shift ;;
    --plan=*)
      plan="${arg#--plan=}"
      shift ;;
    --print-shard)
      print_shard=true
      shift ;;
    --rerun-fails=*)
      rerun_fails="${arg#--rerun-fails=}"
      shift ;;
    *)
      args+=("$arg") ;;
  esac
done

if [ ${#args[@]} -eq 0 ]; then
  args=("-tags=inv.debug" "./...")
fi

# The container-heavy packages run an order of magnitude slower under a full
# parallel suite than they do alone. Go's 10m default kills the whole test
# binary with a panic, so every result in that package is lost; a higher
# ceiling keeps the run reporting real pass/fail.
has_timeout=false
for arg in "${args[@]}"; do
  case $arg in
    -timeout|-timeout=*|--timeout|--timeout=*)
      has_timeout=true ;;
  esac
done

if [ "$has_timeout" = false ]; then
  args=("-timeout=20m" "${args[@]}")
fi

# A rerun invokes 'go test' again for the failed package alone, which overwrites
# cover.out with a profile covering only that package. Rather than report
# coverage that is quietly missing everything else, refuse the combination.
if [ "$cover" = true ] && [ -n "$rerun_fails" ]; then
  echo "--cover and --rerun-fails cannot be combined: a rerun overwrites the coverage profile." >&2
  exit 1
fi

# --shard-changed=<file> narrows the run to the packages a change reaches in
# one step: the packages the files listed in <file> belong to (one path per
# line, relative to the repository root) and the packages importing one of
# those directly. It only applies together with --shard or --plan. See
# ci/cmd/shard for the rules; the two lists below are this suite's part of
# them.
#
# A change to one of these selects every package: tests depend on them without
# importing anything (testenv loads both schemas into every database it
# starts), or they change how everything is built or run.
selection_full_on=(
  go.mod
  go.sum
  mise.toml
  .github/
  .mise-tasks/
  server/database/schema.sql
  server/migrations/
  server/clickhouse/
  local/clickhouse/
)
# Tests in these packages read a file they do not import.
selection_readers=(
  server/gen/http/openapi3.yaml=server/internal/agentmanagement
  server/gen/http/openapi3.yaml=server/internal/killswitchapi
  server/gen/http/openapi3.yaml=server/internal/productfeatures
)

selection=()
if [ -n "$shard_changed" ]; then
  # The paths CI runs this suite for (.github/filters.yaml). A changed file
  # outside them, such as a changeset or dashboard source, is ignored instead
  # of counting as a file no package owns. "dir/**" becomes "dir/".
  selection_scope=$(yq -r '.server[]' ../.github/filters.yaml | sed 's|/\*\*$|/|' | paste -sd, -)
  if [ -z "$selection_scope" ]; then
    echo "No server paths found in .github/filters.yaml" >&2
    exit 1
  fi

  selection=(
    -changed "$shard_changed"
    -scope "$selection_scope"
    -full-on "$(IFS=,; echo "${selection_full_on[*]}")"
    -readers "$(IFS=,; echo "${selection_readers[*]}")"
  )
fi

# --shard=<index>/<total> runs a deterministic subset of the packages that have
# tests, so CI can spread the suite over several runners. See ci/cmd/shard for
# how packages are distributed. --shard-isolate=<import path>[,...] gives each
# listed package a shard of its own, taking the last shards.
if [ -n "$shard" ] || [ -n "$rerun_fails" ] || [ -n "$plan" ]; then
  # Only package patterns are sharded. Everything else reaches 'go test' in the
  # order it was given, including values that follow a flag — the 'TestFoo' in
  # '-run TestFoo' is not a package. Build tags also go to the shard tool so it
  # and 'go test' agree on which packages and test files exist.
  flags=()
  patterns=()
  tags=()
  tags_next=false
  for arg in "${args[@]}"; do
    if [ "$tags_next" = true ]; then
      tags_next=false
      tags+=("-tags=$arg")
      flags+=("$arg")
      continue
    fi

    case $arg in
      -tags=*|--tags=*)
        tags+=("$arg")
        flags+=("$arg") ;;
      -tags|--tags)
        tags_next=true
        flags+=("$arg") ;;
      # Relative paths, anything ending in "..." and import paths such as
      # example.com/mod/pkg. A bare word like "TestFoo" is a flag value.
      ./*|../*|/*|.|*...|*.*/*)
        patterns+=("$arg") ;;
      *)
        flags+=("$arg") ;;
    esac
  done

  packages=("${patterns[@]}")

  # --plan=<max shards> prints, as JSON, how many shards the selection needs
  # and runs nothing. CI builds the server-test matrix from it.
  if [ -n "$plan" ]; then
    exec go run -buildvcs=false github.com/speakeasy-api/gram/ci/cmd/shard plan \
      -max "$plan" "${selection[@]}" "${tags[@]}" "${patterns[@]}"
  fi

  if [ -n "$shard" ]; then
    shard_packages=$(go run -buildvcs=false github.com/speakeasy-api/gram/ci/cmd/shard \
      -i "$shard" -isolate "$shard_isolate" "${selection[@]}" "${tags[@]}" "${patterns[@]}") || exit $?

    # --print-shard lists the shard's packages and runs nothing.
    if [ "$print_shard" = true ]; then
      echo "$shard_packages"
      exit 0
    fi

    packages=()
    while IFS= read -r line; do
      [ -n "$line" ] && packages+=("$line")
    done <<< "$shard_packages"

    if [ ${#packages[@]} -eq 0 ]; then
      echo "Shard $shard: no packages to test."
      exit 0
    fi
  fi

  args=("${flags[@]}" "${packages[@]}")
fi

if [ "$cover" = true ]; then
  args=("-coverprofile=cover.out" "-covermode=atomic" "${args[@]}")
fi

gotestsum_flags=(--junitfile junit-report.xml --format-hide-empty-pkg)

# --rerun-fails=<n> re-runs only the tests that failed, up to n more times, so a
# flaky test does not fail the run on its own. gotestsum needs the packages
# separately from the 'go test' flags to do that, and skips reruns entirely when
# the first attempt failed more than --rerun-fails-max-failures times (i.e. the
# suite is broken rather than flaky).
if [ -n "$rerun_fails" ]; then
  gotestsum_flags+=(
    --rerun-fails="$rerun_fails"
    --packages="${packages[*]}"
    # Names the tests that only passed on a retry. Without it a flake is
    # invisible: the run is green and nothing says a test had to be re-run.
    --rerun-fails-report=rerun-report.txt
  )
  # gotestsum builds the 'go test' command itself for every attempt, so the
  # packages move to --packages and only the flags stay behind.
  args=("${flags[@]}")
fi

gotestsum "${gotestsum_flags[@]}" -- "${args[@]}"
test_exit_code=$?

if [ "$cover" = true ] && [ -f "cover.out" ]; then
  grep -v "/gen/" cover.out > coverage_filtered.out
  mv coverage_filtered.out cover.out

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
