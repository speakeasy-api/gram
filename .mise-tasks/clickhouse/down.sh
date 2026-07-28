#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Undo a versioned migration"
#USAGE arg "<target>" help="The target previous migration to go down to"

set -e

if [ "${usage_target:-}" = "" ]; then
  echo "Usage: $0 <target>"
  exit 1
fi

# Atlas resolves the docker:// dev-url through the Docker API; make sure the
# rootless podman socket is up and DOCKER_HOST points at it.
# shellcheck source=../../local/lib/compose.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/local/lib/compose.sh"
ensure_podman_socket

exec atlas migrate down --to-version "${usage_target:?}" \
  --dir file://clickhouse/migrations \
  --config file://atlas.hcl \
  --url "$GRAM_CLICKHOUSE_URL" \
  --dev-url "docker://clickhouse/clickhouse-server/25.8.3/dev"
