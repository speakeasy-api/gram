#!/usr/bin/env bash

# Blocking SessionStart preflight: fresh installs wait here until explicit or
# cached hook credentials are available, then later hook senders can reuse them.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"
project_slug="${GRAM_HOOKS_PROJECT_SLUG:-${GRAM_PROJECT_SLUG:-}}"
gram_hooks_org_hint="${GRAM_HOOKS_ORG_ID:-}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if ! . "$script_dir/auth.sh"; then
  echo "Speakeasy hooks could not load auth helper." >&2
  exit 2
fi

export GRAM_HOOKS_INTERACTIVE=1

# Never-authenticated machines fail open (prepare_auth returns 3 after
# warning); once credentials have been established, a broken auth state
# still exits 2 from inside prepare_auth and blocks the session start.
gram_hooks_prepare_auth "$server_url" "$project_slug" 2 || true
exit 0
