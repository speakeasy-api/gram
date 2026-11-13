#!/usr/bin/env bash

#MISE dir="{{ config_root }}/sc"
#MISE description="Benchmark the sc statechart package in memory against the same machines written by hand. Extra arguments go to 'go test', for example -bench Core or -count 5."

set -e

exec go test -run '^$' -bench . -benchmem ./internal/scbench "$@"
