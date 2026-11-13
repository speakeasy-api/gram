#!/usr/bin/env bash

#MISE dir="{{ config_root }}/sc"
#MISE description="Test the sc statechart package with the race detector. It takes the same arguments as 'go test' and tests every package unless one is named. The pgdriver tests start a Postgres container, so Docker must be running."

set -e

# Flags alone, such as -count=1, still test every package. A flag's value
# that looks like a path, such as -coverprofile ./cover.out, is not a
# package: only a directory or a ... pattern is.
has_package=false
for arg in "$@"; do
  case "$arg" in
    . | */...) has_package=true ;;
    ./*) if [ -d "$arg" ]; then has_package=true; fi ;;
  esac
done
if [ "$has_package" = false ]; then
  set -- "$@" "./..."
fi

exec gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- -race "$@"
