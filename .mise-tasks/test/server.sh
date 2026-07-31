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
  # Package patterns are sharded; every other argument is passed to 'go test'
  # as-is. Build tags also go to the shard tool so it and 'go test' agree on
  # which packages and test files exist.
  flags=()
  patterns=()
  tags=()
  for arg in "${args[@]}"; do
    case $arg in
      -tags=*|--tags=*)
        tags+=("$arg")
        flags+=("$arg") ;;
      -*)
        flags+=("$arg") ;;
      *)
        patterns+=("$arg") ;;
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
