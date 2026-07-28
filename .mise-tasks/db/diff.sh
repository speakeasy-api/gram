#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Create a versioned migration"
#USAGE arg "<name>" help="The name of the migration"

set -e

if [ "${usage_name:-}" = "" ]; then
  echo "Usage: $0 --name <name>"
  exit 1
fi

# Atlas resolves the docker:// dev-url through the Docker API; make sure the
# rootless podman socket is up and DOCKER_HOST points at it.
# shellcheck source=../../local/lib/compose.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/local/lib/compose.sh"
ensure_podman_socket

exec atlas migrate diff "${usage_name:?}" \
  --config file://atlas.hcl \
  --to file://database/schema.sql \
  --dev-url "docker://pgvector/pgvector/pg17/dev?search_path=public"