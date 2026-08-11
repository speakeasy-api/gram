#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Run local Platform MCP smoke checks; setup is the only persistent phase."
#USAGE arg "<phase>" help="Smoke phase: setup, read-only, registration, readiness, full, or all" default="read-only" {
#USAGE   choices "setup" "read-only" "registration" "readiness" "full" "all"
#USAGE }

set -euo pipefail

phase="${1:-read-only}"
feature_name="platform_mcp"
rollout_flag="platform-mcp-rollout"
registration_flag="platform-mcp-catalog-registration"

fail() {
  echo "platform-mcp smoke: $*" >&2
  exit 1
}

require_local_fixture_targets() {
  [ "${GRAM_ENVIRONMENT:-}" = "local" ] || fail "setup requires GRAM_ENVIRONMENT=local"
  [ "${GRAM_SERVER_URL:-}" != "" ] || fail "GRAM_SERVER_URL is required"
  [ "${GRAM_DATABASE_URL:-}" != "" ] || fail "GRAM_DATABASE_URL is required"
  [ "${GRAM_LOCAL_FEATURE_FLAGS_CSV:-}" != "" ] || fail "GRAM_LOCAL_FEATURE_FLAGS_CSV is required"
  [ "${GRAM_REDIS_CACHE_ADDR:-}" != "" ] || fail "GRAM_REDIS_CACHE_ADDR is required"
  [ "${NODE_EXTRA_CA_CERTS:-}" != "" ] || fail "NODE_EXTRA_CA_CERTS is required for the local HTTPS self-call"
  [ -f "$NODE_EXTRA_CA_CERTS" ] || fail "local CA file does not exist"

  case "$GRAM_SERVER_URL" in
    https://localhost:*|https://127.0.0.1:*) ;;
    *) fail "GRAM_SERVER_URL must be an HTTPS loopback URL" ;;
  esac
  case "$GRAM_DATABASE_URL" in
    *"@localhost:"*|*"@127.0.0.1:"*|*"@::1:"*) ;;
    *) fail "GRAM_DATABASE_URL must target a loopback database" ;;
  esac
  case "$GRAM_REDIS_CACHE_ADDR" in
    localhost:*|127.0.0.1:*|[::1]:*) ;;
    *) fail "GRAM_REDIS_CACHE_ADDR must target loopback" ;;
  esac
  case "$GRAM_LOCAL_FEATURE_FLAGS_CSV" in
    "$PWD"/server/*) ;;
    *) fail "GRAM_LOCAL_FEATURE_FLAGS_CSV must be under server/" ;;
  esac
  [ -f "$GRAM_LOCAL_FEATURE_FLAGS_CSV" ] || fail "local feature CSV does not exist"

  curl --fail --silent --show-error --cacert "$NODE_EXTRA_CA_CERTS" \
    --output /dev/null "$GRAM_SERVER_URL/rpc/auth.login"
}

json_field() {
  field="$1"
  mise exec -- node -e '
const field = process.argv[1];
let value = JSON.parse(require("fs").readFileSync(0, "utf8"));
for (const part of field.split(".")) {
  if (value == null || typeof value !== "object") process.exit(1);
  value = value[part];
}
if (typeof value !== "string" || value === "") process.exit(1);
process.stdout.write(value);
' "$field"
}

local_session() {
  login_headers_file="$(mktemp)"
  authorize_headers_file="$(mktemp)"
  callback_headers_file="$(mktemp)"
  trap 'rm -f "$login_headers_file" "$authorize_headers_file" "$callback_headers_file"' RETURN

  curl --silent --show-error --cacert "$NODE_EXTRA_CA_CERTS" --output /dev/null --dump-header "$login_headers_file" \
    "$GRAM_SERVER_URL/rpc/auth.login"
  nonce_cookie="$(awk 'tolower($1) == "set-cookie:" && $0 ~ /gram_auth_nonce=/ { sub(/^[^:]*: /, ""); sub(/;.*/, ""); print; exit }' "$login_headers_file")"
  authorize_url="$(awk 'tolower($1) == "location:" { sub(/^[^:]*: /, ""); sub(/\r$/, ""); print; exit }' "$login_headers_file")"
  [ -n "$nonce_cookie" ] || fail "local auth login did not set a nonce cookie"
  [ -n "$authorize_url" ] || fail "local auth login did not return an authorization URL"

  curl --silent --show-error --cacert "$NODE_EXTRA_CA_CERTS" --output /dev/null --dump-header "$authorize_headers_file" "$authorize_url"
  callback_url="$(awk 'tolower($1) == "location:" { sub(/^[^:]*: /, ""); sub(/\r$/, ""); print; exit }' "$authorize_headers_file")"
  [ -n "$callback_url" ] || fail "local dev-idp did not return an auth callback"

  curl --silent --show-error --cacert "$NODE_EXTRA_CA_CERTS" --output /dev/null --dump-header "$callback_headers_file" \
    --header "Cookie: $nonce_cookie" "$callback_url"
  session="$(awk 'tolower($1) == "gram-session:" { sub(/^[^:]*: /, ""); sub(/\r$/, ""); print; exit }' "$callback_headers_file")"
  [ -n "$session" ] || fail "local auth callback did not return a session"
  printf '%s' "$session"
}

setup() {
  require_local_fixture_targets
  session="$(local_session)"
  info="$(curl --fail --silent --show-error --cacert "$NODE_EXTRA_CA_CERTS" \
    --header "Gram-Session: $session" "$GRAM_SERVER_URL/rpc/auth.info")"
  organization_id="$(printf '%s' "$info" | json_field 'active_organization_id')"
  [ "$organization_id" != "acme-demo" ] || fail "setup refuses the read-only demo organization"

  # This authenticated org-admin route is the live authorization check and
  # preserves the normal feature audit/cache behavior before the exact cache
  # key is removed below for any separate local process.
  curl --fail --silent --show-error --cacert "$NODE_EXTRA_CA_CERTS" \
    --header "Content-Type: application/json" \
    --header "Gram-Session: $session" \
    --data '{"feature_name":"platform_mcp","enabled":true}' \
    "$GRAM_SERVER_URL/rpc/productFeatures.set" >/dev/null

  redis_password="${GRAM_REDIS_CACHE_PASSWORD:-xi9XILbY}"
  docker compose exec -T gram-cache redis-cli -p 35299 -a "$redis_password" \
    DEL "feature:${organization_id}:${feature_name}:" >/dev/null

  csv_tmp="$(mktemp)"
  trap 'rm -f "$csv_tmp"' RETURN
  awk -F, -v OFS=, -v organization_id="$organization_id" \
    -v rollout_flag="$rollout_flag" -v registration_flag="$registration_flag" '
      NR == 1 { print; next }
      !($1 == organization_id && ($2 == rollout_flag || $2 == registration_flag)) { print }
    ' "$GRAM_LOCAL_FEATURE_FLAGS_CSV" > "$csv_tmp"
  printf '%s,%s,true\n%s,%s,true\n' \
    "$organization_id" "$rollout_flag" "$organization_id" "$registration_flag" >> "$csv_tmp"
  mv "$csv_tmp" "$GRAM_LOCAL_FEATURE_FLAGS_CSV"

  echo "Platform MCP local test gates prepared."
  echo "Restart the local server with GRAM_PLATFORM_MCP_LOCAL_FIXTURE=1 before browser acceptance."
}

read_only() {
  mise exec -- go test ./server/internal/plugins -run '^TestPluginsService_PublishProject_PlatformMCPAdmissionTransitions$' -count=1
  mise exec -- go test ./server/internal/platformmcp -run '^(TestOAuthHTTPCompletesChallengeStateHandoff|TestRegistrationStoreCompleteRegistrationConvergesPrivateComponents|TestRegistrationStoreDoesNotCountPendingRegistrationsTowardActiveCap|TestRuntimeHandlerRecordsReadyAfterSuccessfulToolsList|TestServiceRevokeConnection|TestServiceGetLifecycleDoesNotExposeCredentials|TestRepairActionsAreBoundedAndStateSpecific|TestReadinessFreshnessIsSeparateFromState)$' -count=1
}

case "$phase" in
  setup)
    setup
    ;;
  read-only)
    read_only
    ;;
  registration)
    mise exec -- go test ./server/internal/platformmcp -run '^(TestRegistrationStoreCompleteRegistrationConvergesPrivateComponents|TestRegistrationStoreDoesNotCountPendingRegistrationsTowardActiveCap|TestRegistrationStoreEnforcesActiveRegistrationCap|TestRegistrationStoreSerializesCapRejectionsForDistinctCandidates)$' -count=1
    ;;
  readiness)
    mise exec -- go test ./server/internal/platformmcp -run '^(TestRuntimeHandlerRecordsReadyAfterSuccessfulToolsList|TestServiceRevokeConnection|TestServiceGetLifecycleDoesNotExposeCredentials|TestRepairActionsAreBoundedAndStateSpecific|TestReadinessFreshnessIsSeparateFromState)$' -count=1
    ;;
  full|all)
    read_only
    mise exec -- go test ./server/internal/platformmcp -run '^(TestRegistrationStoreEnforcesActiveRegistrationCap|TestRegistrationStoreSerializesCapRejectionsForDistinctCandidates)$' -count=1
    ;;
  *)
    echo "usage: mise run smoke:platform-mcp [setup|read-only|registration|readiness|full|all]" >&2
    exit 2
    ;;
esac
