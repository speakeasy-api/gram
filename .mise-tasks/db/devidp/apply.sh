#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Apply the dev-idp Postgres schema declaratively (atlas schema apply, no migration files)"

set -e

if [ -z "${GRAM_DEVIDP_DATABASE_URL:-}" ]; then
  echo "GRAM_DEVIDP_DATABASE_URL is not set" >&2
  exit 1
fi

exec atlas schema apply \
  --url "${GRAM_DEVIDP_DATABASE_URL}" \
  --to "file://internal/devidp/database/schema.sql" \
  --dev-url "docker://pgvector/pgvector/pg17/dev?search_path=public" \
  --auto-approve
