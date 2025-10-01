#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Regenerate Clickhouse atlas.sum"

set -e

exec atlas migrate hash \
  --dir file://clickhouse/migrations \
  --config file://atlas.hcl
