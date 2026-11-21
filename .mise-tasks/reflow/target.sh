#!/usr/bin/env bash

#MISE description="Point Reflow at this worktree's running stack, or at a deployed non-production host, so golden flows can run against it"

#USAGE flag "--collection <id>" env="REFLOW_COLLECTION" required=#true help="Reflow application collection ID"
#USAGE flag "--url <url>" help="Deployed non-production host to target instead of this worktree's stack, e.g. https://pr-123.dev.getgram.ai"
#USAGE flag "--expect-build <sha>" help="Required with --url: commit the host must serve before the suite's root goes on (a short SHA is enough)"
#USAGE flag "--group <name>" help="Reflow target group (default: local-<worktree directory>, or the deployed host name)"
#USAGE flag "--dir <dir>" help="Private directory for bindings and inputs (default: ~/.reflow/gram/<group>)"

# Writes the private bindings and the flow inputs the golden suite reads, then
# selects the target.
#
# Every target gets:
#   site  Browser, at the dashboard URL.
#   api   OpenAPI, pinned to server/internal/e2econtrol/openapi.yaml: the
#         end-to-end control surface (reseed, sign-in link). It calls the
#         dashboard origin, which serves /rpc in every environment (the Vite
#         proxy locally, the ingress when deployed), so no extra origin is
#         granted. Its one credential is the control token.
#
# This worktree's stack (no --url) also gets:
#   db    PostgreSQL, read-only, for the flows that still read SQL, and the
#         extra origins the local dev-idp sign-in redirect visits.
#
# The control surface exists on a local stack that set a token, and on a pull
# request preview that opted in (the preview-e2e label). Nowhere else: --url
# pointed at any other host cannot pass the suite's root.
#
# A deployed host (--url) gets no database binding: nothing in CI holds a DSN
# for a deployed environment, and flows that need `db` are not selected there.
# Its credential comes from the environment:
#   GRAM_E2E_CONTROL_TOKEN  the control token of the environment's previews
#
# The bindings hold a credential, so they live outside the checkout, owner-only.

set -euo pipefail

if ! command -v reflow >/dev/null 2>&1; then
  echo "reflow is not on PATH. Install it: https://reflow.io/docs/agents-start-here/" >&2
  exit 1
fi

origin() {
  # scheme://host:port, without any path, query or fragment.
  printf '%s' "$1" | sed -E 's|^([a-z]+://[^/?#]+).*|\1|'
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

collection="${usage_collection:?}"
remote_url="${usage_url:-}"

spec_file="$PWD/server/internal/e2econtrol/openapi.yaml"
if [ ! -f "$spec_file" ]; then
  echo "missing $spec_file; run this from the repository root through mise" >&2
  exit 1
fi
spec_sha="$(sha256 "$spec_file")"

if [ -n "$remote_url" ]; then
  case "$remote_url" in
  https://*) ;;
  *)
    echo "--url must be an https URL, got: $remote_url" >&2
    exit 1
    ;;
  esac
  : "${GRAM_E2E_CONTROL_TOKEN:?GRAM_E2E_CONTROL_TOKEN is not set; it is the end-to-end control token that opted-in previews are given}"
  # A run against a deployed host always says which build it is testing, so a
  # pass is never recorded against whatever happened to be serving.
  expect_build="${usage_expect_build:?--expect-build is required with --url}"
  site_url="$(origin "$remote_url")"
  default_group="${site_url#*://}"
else
  : "${GRAM_SITE_URL:?GRAM_SITE_URL is not set; run this through mise}"
  : "${GRAM_SERVER_URL:?GRAM_SERVER_URL is not set; run this through mise}"
  : "${GRAM_DEVIDP_EXTERNAL_URL:?GRAM_DEVIDP_EXTERNAL_URL is not set; run this through mise}"
  : "${GRAM_DATABASE_URL:?GRAM_DATABASE_URL is not set; run this through mise}"
  # The control surface is opt-in: nothing sets its token by default, and the
  # server reads it only at startup.
  if [ -z "${GRAM_E2E_CONTROL_TOKEN:-}" ]; then
    echo "GRAM_E2E_CONTROL_TOKEN is not set. The end-to-end control surface is opt-in:" >&2
    echo "generate a private token for this stack, then restart the server so it reads it:" >&2
    echo "  mise set --file mise.local.toml GRAM_E2E_CONTROL_TOKEN=\"\$(openssl rand -hex 24)\"" >&2
    echo "  pitchfork restart server" >&2
    exit 1
  fi
  site_url="$(origin "$GRAM_SITE_URL")"
  default_group="local-$(basename "$PWD")"
fi

group="${usage_group:-$default_group}"
dir="${usage_dir:-$HOME/.reflow/gram/$group}"

umask 077
mkdir -p "$dir"
chmod 700 "$dir"

# The credential reaches jq through the environment rather than its command line.
CONTROL_TOKEN="$GRAM_E2E_CONTROL_TOKEN" jq -n \
  --arg site "$site_url" \
  --arg spec "$spec_file" \
  --arg sha "$spec_sha" \
  '{
    site: {provider: "Browser", configuration: {targetUrl: $site, browser: "chromium", width: 1440, height: 900}},
    api: {provider: "OpenAPI", configuration: {
      specFile: $spec, specSha256: $sha, baseUrl: $site, timeoutMs: 120000,
      credentials: {control: {value: env.CONTROL_TOKEN}}
    }}
  }' >"$dir/bindings.json"

if [ -n "$remote_url" ]; then
  jq -n --arg host "${site_url#*://}" --arg build "$expect_build" \
    '{gramServerHost: $host, expectedBuild: $build}' >"$dir/inputs.json"
  chmod 600 "$dir/bindings.json" "$dir/inputs.json"

  reflow target "$site_url" \
    --group "$group" \
    --collection "$collection" \
    --scope device \
    --bindings "$dir/bindings.json" \
    --inputs "$dir/inputs.json"
  exit 0
fi

server_url="$(origin "$GRAM_SERVER_URL")"
idp_url="$(origin "$GRAM_DEVIDP_EXTERNAL_URL")"
server_host="${server_url#*://}"

# The Reflow PostgreSQL plugin refuses the search_path query parameter that
# GRAM_DATABASE_URL carries, so keep only its sslmode. The local stack's
# Postgres has no TLS, so a URL without one gets sslmode=disable.
sslmode=""
case "$GRAM_DATABASE_URL" in
*\?*) sslmode="$(printf '%s' "${GRAM_DATABASE_URL#*\?}" | tr '&' '\n' | sed -n 's/^sslmode=//p' | head -n1)" ;;
esac
dsn="${GRAM_DATABASE_URL%%\?*}?sslmode=${sslmode:-disable}"

# The DSN carries the database password, so it reaches jq through the
# environment rather than its command line.
with_db="$(mktemp "$dir/bindings.XXXXXX")"
DSN="$dsn" jq \
  '. + {db: {provider: "PostgreSQL", configuration: {connectionString: env.DSN, allowWrites: false}}}' \
  "$dir/bindings.json" >"$with_db"
mv "$with_db" "$dir/bindings.json"

# A local server is built without a commit stamp and reports "dev".
jq -n --arg host "$server_host" '{gramServerHost: $host, expectedBuild: "dev"}' >"$dir/inputs.json"

chmod 600 "$dir/bindings.json" "$dir/inputs.json"

reflow target "$site_url" \
  --group "$group" \
  --collection "$collection" \
  --scope device \
  --bindings "$dir/bindings.json" \
  --inputs "$dir/inputs.json" \
  --allow-origin "$server_url" \
  --allow-origin "$idp_url"
