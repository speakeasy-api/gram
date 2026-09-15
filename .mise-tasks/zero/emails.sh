#!/usr/bin/env bash

#MISE description="Resolve existing Loops transactional email IDs for local development"
#MISE dir="{{ config_root }}"

#USAGE flag "--api-key <api-key>" env="LOOPS_API_KEY" required=#true help="Loops API key (defaults to LOOPS_API_KEY)"

set -euo pipefail

email_ids_file="$(mktemp)"
trap 'rm -f "$email_ids_file"' EXIT

# shellcheck disable=SC2154 # Populated by mise from the usage specification.
LOOPS_API_KEY="$usage_api_key" \
  ./server/internal/email/loops/sync.sh \
  --resolve-only \
  --output "$email_ids_file"

email_ids="$(jq -e -c '
  if type == "object" and length > 0 then
    .
  else
    error("expected a non-empty template ID map")
  end
' "$email_ids_file")"

mise set --file mise.local.toml "GRAM_EMAIL_TEMPLATE_IDS=$email_ids"

echo "✅ Resolved Loops transactional email IDs and updated GRAM_EMAIL_TEMPLATE_IDS in mise.local.toml."
