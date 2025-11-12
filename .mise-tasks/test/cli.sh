#!/usr/bin/env bash
#MISE dir="{{ config_root }}/cli"
#MISE description="Test the cli"

if [ $# -eq 0 ]; then
  set -- "-tags=inv.debug" "./..."
fi

exec gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- "$@"