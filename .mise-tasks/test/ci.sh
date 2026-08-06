#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Test the CI helper commands under ci/"

set -e

go vet ./ci/...
gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- ./ci/...
