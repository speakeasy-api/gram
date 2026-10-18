#!/usr/bin/env bash

#MISE description="Benchmark canonical signal queries on an isolated ClickHouse 26.6 with synthetic data"
#MISE dir="{{ config_root }}/server"
#USAGE flag "--readings-per-day <count>" default="1000"
#USAGE flag "--days <count>" default="730"
#USAGE flag "--sensors <count>" default="3"
#USAGE flag "--iterations <count>" default="5"
#USAGE flag "--event-time-index" help="Measure a source-time minmax index in the disposable database"
#USAGE flag "--out <file>" default="/tmp/opencode/sigint-benchmark.json"

set -euo pipefail
exec go run ./cmd/sigint-bench \
  -readings-per-day "${usage_readings_per_day}" -days "${usage_days}" \
  -sensors "${usage_sensors}" -iterations "${usage_iterations}" -out "${usage_out}" \
  -event-time-index="${usage_event_time_index:-false}"
