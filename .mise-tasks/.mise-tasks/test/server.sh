#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Test the server"
#MISE sources=["server/**/*.go"]

if [ $# -eq 0 ]; then
  set -- "-tags=inv.debug" "./..."
fi

exec gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- "$@"