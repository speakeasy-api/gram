#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Test the CI helper commands under ci/ and the sqlclint linter"

set -e

go vet ./ci/... ./sqlclint/...
gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- ./ci/... ./sqlclint/...
