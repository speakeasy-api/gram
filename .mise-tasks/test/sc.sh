#!/usr/bin/env bash

#MISE dir="{{ config_root }}/sc"
#MISE description="Test the sc statechart package with the race detector. It takes the same arguments as 'go test' and defaults to every package. The pgdriver tests start a Postgres container, so Docker must be running."

set -e

if [ $# -eq 0 ]; then
  set -- "./..."
fi

exec gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- -race "$@"
