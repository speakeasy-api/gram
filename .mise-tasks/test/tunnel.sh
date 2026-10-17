#!/usr/bin/env bash

#MISE description="Test the tunnel gateway and agent"
#MISE dir="{{ config_root }}"

set -euo pipefail

if [ $# -eq 0 ]; then
  set -- -race ./tunnel/...
fi

gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- "$@"
