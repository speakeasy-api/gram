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
  --dev-url "docker://clickhouse/clickhouse-server/26.4.5.143-distroless@sha256:d7c4cf5575c3d0d49cf1beb7ce8d938d64f707789d9fa3fa538c7d3f898b0649/"
