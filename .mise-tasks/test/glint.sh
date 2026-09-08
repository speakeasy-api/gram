#!/usr/bin/env bash

#MISE dir="{{ config_root }}/glint"
#MISE description="Test the glint analyzers (the golangci-lint plugin module)"

set -e

exec gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- ./...
