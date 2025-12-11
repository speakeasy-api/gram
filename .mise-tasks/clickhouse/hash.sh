#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Regenerate Clickhouse atlas.sum"

set -e

atlas migrate hash \
  --dir file://clickhouse/migrations \
  --config file://atlas.hcl

exec atlas migrate hash \
  --dir file://clickhouse/local/golang_migrate?format=golang-migrate \
  --config file://atlas.hcl
