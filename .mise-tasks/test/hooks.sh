#!/usr/bin/env bash

#MISE dir="{{ config_root }}/hooks"
#MISE description="Vet and test the speakeasy-hooks packages"

set -e

go vet ./...
gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- ./...
