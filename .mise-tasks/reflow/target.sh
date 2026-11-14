#!/usr/bin/env bash

#MISE description="Point Reflow at this worktree's running stack so golden flows can run against it"

#USAGE flag "--collection <id>" env="REFLOW_COLLECTION" required=#true help="Reflow application collection ID"
#USAGE flag "--group <name>" help="Reflow target group (default: local-<worktree directory>)"
#USAGE flag "--dir <dir>" help="Private directory for bindings and inputs (default: ~/.reflow/gram/<group>)"

# Writes the private Browser and PostgreSQL bindings and the flow inputs the
# golden suite reads, then selects the target with the extra origins that the
# local sign-in redirect visits (the server and the dev identity provider).
# The bindings hold a database DSN, so they live outside the checkout, owner-only.

set -euo pipefail

: "${GRAM_SITE_URL:?GRAM_SITE_URL is not set; run this through mise}"
: "${GRAM_SERVER_URL:?GRAM_SERVER_URL is not set; run this through mise}"
: "${GRAM_DEVIDP_EXTERNAL_URL:?GRAM_DEVIDP_EXTERNAL_URL is not set; run this through mise}"
: "${GRAM_DATABASE_URL:?GRAM_DATABASE_URL is not set; run this through mise}"

if ! command -v reflow >/dev/null 2>&1; then
  echo "reflow is not on PATH. Install it: https://reflow.io/docs/agents-start-here/" >&2
  exit 1
fi

collection="${usage_collection:?}"
group="${usage_group:-local-$(basename "$PWD")}"
dir="${usage_dir:-$HOME/.reflow/gram/$group}"

origin() {
  # scheme://host:port, without any path.
  printf '%s' "$1" | sed -E 's#^([a-z]+://[^/]+).*#\1#'
}

site_url="$(origin "$GRAM_SITE_URL")"
server_url="$(origin "$GRAM_SERVER_URL")"
idp_url="$(origin "$GRAM_DEVIDP_EXTERNAL_URL")"
server_host="${server_url#*://}"

umask 077
mkdir -p "$dir"
chmod 700 "$dir"

# The Reflow PostgreSQL plugin refuses the search_path query parameter that
# GRAM_DATABASE_URL carries, so keep only sslmode.
dsn="${GRAM_DATABASE_URL%%\?*}?sslmode=disable"

jq -n \
  --arg site "$site_url" \
  --arg dsn "$dsn" \
  '{
    site: {provider: "Browser", configuration: {targetUrl: $site, browser: "chromium", width: 1440, height: 900}},
    db: {provider: "PostgreSQL", configuration: {connectionString: $dsn, allowWrites: false}}
  }' >"$dir/bindings.json"

jq -n --arg host "$server_host" '{gramServerHost: $host}' >"$dir/inputs.json"

chmod 600 "$dir/bindings.json" "$dir/inputs.json"

reflow target "$site_url" \
  --group "$group" \
  --collection "$collection" \
  --scope device \
  --bindings "$dir/bindings.json" \
  --inputs "$dir/inputs.json" \
  --allow-origin "$server_url" \
  --allow-origin "$idp_url"
