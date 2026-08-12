#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Test the server with optional coverage generation. It takes the same arguments as 'go test'."

# Check if flags are provided
cover=false
open_html=false
shard=""
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
    *)
      args+=("$arg") ;;
  esac
done

if [ ${#args[@]} -eq 0 ]; then
  args=("-tags=inv.debug" "./...")
fi

# --shard=<index>/<total> runs a deterministic subset of the packages that have
# tests, so CI can spread the suite over several runners. See ci/cmd/shard for
# how packages are distributed.
if [ -n "$shard" ]; then
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

  shard_packages=$(go run github.com/speakeasy-api/gram/ci/cmd/shard \
    -i "$shard" "${tags[@]}" "${patterns[@]}") || exit $?

  packages=()
  while IFS= read -r line; do
    [ -n "$line" ] && packages+=("$line")
  done <<< "$shard_packages"

  if [ ${#packages[@]} -eq 0 ]; then
    echo "Shard $shard: no packages to test."
    exit 0
  fi

  args=("${flags[@]}" "${packages[@]}")
fi

if [ "$cover" = true ]; then
  args=("-coverprofile=cover.out" "-covermode=atomic" "${args[@]}")
fi

gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- "${args[@]}"
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
