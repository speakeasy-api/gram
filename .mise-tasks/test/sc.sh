#!/usr/bin/env bash

#MISE dir="{{ config_root }}/sc"
#MISE description="Test the sc statechart package with the race detector. It takes the same arguments as 'go test' and tests every package unless one is named. The pgdriver tests start a Postgres container, so Docker must be running."

set -e

# Flags alone, such as -count=1, still test every package.
has_package=false
for arg in "$@"; do
  case "$arg" in
    . | ./* | */...) has_package=true ;;
  esac
done
if [ "$has_package" = false ]; then
  set -- "$@" "./..."
fi

exec gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- -race "$@"
