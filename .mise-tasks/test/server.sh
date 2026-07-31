#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Test the server with optional coverage generation. It takes the same arguments as 'go test'."

# Check if flags are provided
cover=false
open_html=false
args=()

for arg in "$@"; do
  case $arg in
    --cover)
      cover=true
      shift ;;
    --html)
      open_html=true
      shift ;;
    *)
      args+=("$arg") ;;
  esac
done

if [ ${#args[@]} -eq 0 ]; then
  args=("-tags=inv.debug" "./...")
fi

if [ "$cover" = true ]; then
  args=("-coverprofile=cover.out" "-covermode=atomic" "${args[@]}")
fi

# Explicit package/test parallelism from CPU count (bash 3.2+ / macOS-safe).
# Callers can still override by passing -p / -parallel themselves.
cpus=$(getconf _NPROCESSORS_ONLN 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 1)
case "$cpus" in
  ''|*[!0-9]*) cpus=1 ;;
  0) cpus=1 ;;
esac

has_p=false
has_parallel=false
for arg in "${args[@]}"; do
  case "$arg" in
    -p|-p=*) has_p=true ;;
    -parallel|-parallel=*) has_parallel=true ;;
  esac
done

if [ "$has_p" = false ]; then
  args=("-p" "$cpus" "${args[@]}")
fi
if [ "$has_parallel" = false ]; then
  args=("-parallel" "$cpus" "${args[@]}")
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
