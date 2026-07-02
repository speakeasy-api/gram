#!/usr/bin/env bash

# Interactive login for Speakeasy observability hooks. Opens a browser to the
# Gram dashboard, waits for the localhost callback, and caches the minted
# hooks API key for this machine. Safe to re-run: exits 0 immediately when
# already authenticated. Pass --force to discard cached credentials first.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"
project_slug="${GRAM_HOOKS_PROJECT_SLUG:-${GRAM_PROJECT_SLUG:-}}"
gram_hooks_org_hint="${GRAM_HOOKS_ORG_ID:-}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
if ! . "$script_dir/auth.sh"; then
  echo "Speakeasy hooks could not load auth helper." >&2
  exit 1
fi

export GRAM_HOOKS_INTERACTIVE=1
export GRAM_HOOKS_LOGIN_FORCE=1

if [ "${1:-}" = "--force" ]; then
  gram_hooks_forget_auth
elif gram_hooks_read_auth "$server_url" 2>/dev/null; then
  echo "Speakeasy hooks already authenticated for ${server_url} (project ${GRAM_HOOKS_CACHED_PROJECT:-unset}). Re-run with --force to re-authenticate."
  exit 0
fi

if ! gram_hooks_login "$server_url" "$project_slug"; then
  echo "Speakeasy hooks login failed. Alternatively set GRAM_HOOKS_API_KEY and GRAM_HOOKS_PROJECT_SLUG in your environment." >&2
  exit 1
fi

if ! gram_hooks_read_auth "$server_url" 2>/dev/null; then
  echo "Speakeasy hooks login completed but credentials could not be read back." >&2
  exit 1
fi

echo "Speakeasy hooks authenticated (project ${GRAM_HOOKS_CACHED_PROJECT:-unset})."
exit 0
