#!/usr/bin/env bash

# Blocking sessionStart preflight: fresh installs wait here until explicit or
# cached hook credentials are available, then later hook senders can reuse them.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"
project_slug="${GRAM_HOOKS_PROJECT_SLUG:-${GRAM_PROJECT_SLUG:-}}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if ! . "$script_dir/auth.sh"; then
  echo "Speakeasy hooks could not load auth helper." >&2
  exit 2
fi

gram_hooks_prepare_auth "$server_url" "$project_slug" 2
exit 0
