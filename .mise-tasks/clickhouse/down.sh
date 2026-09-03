#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Undo a versioned migration"
#USAGE arg "<target>" help="The target previous migration to go down to"

set -e

if [ "${usage_target:-}" = "" ]; then
  echo "Usage: $0 <target>"
  exit 1
fi

exec atlas migrate down --to-version "${usage_target:?}" \
  --dir file://clickhouse/migrations \
  --config file://atlas.hcl \
  --url "$GRAM_CLICKHOUSE_URL" \
  --dev-url "docker://clickhouse/clickhouse-server/26.2.19.43@sha256:c2f2605585899d5103a0447daadbc0005f362200d5f0fcca7f40db3ca0dd36dd/dev"
