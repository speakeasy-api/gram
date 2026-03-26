#!/usr/bin/env bash

#MISE description="Regenerate materialized_columns_gen.go from clickhouse/schema.sql"
#MISE dir="{{ config_root }}/server"
#MISE sources=["clickhouse/schema.sql"]
#MISE outputs=["internal/telemetry/repo/materialized_columns_gen.go"]

set -e

go generate ./internal/telemetry/repo/
