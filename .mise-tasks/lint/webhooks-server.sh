#!/usr/bin/env bash

#MISE description="Check webhooks catalog for breaking changes against a base ref"
#MISE dir="{{ config_root }}"

#USAGE flag "--base <ref>" help="Git ref to compare against" default="origin/main"

set -eo pipefail

CATALOG="server/internal/outbox/events/catalog_gen.yaml"
BASE="${usage_base:-origin/main}"

if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
  BREAKING=$(openapi-changes report --no-logo \
    "${BASE}:${CATALOG}" "${CATALOG}" 2>/dev/null \
    | jq '[.reportSummary | to_entries[] | .value.breakingChanges] | add // 0')

  if [ "${BREAKING:-0}" -gt 0 ]; then
    openapi-changes markdown-report --no-logo \
      --report-file breaking-changes.md \
      --include-diff \
      "${BASE}:${CATALOG}" "${CATALOG}"
  fi
else
  openapi-changes summary "${BASE}:${CATALOG}" "${CATALOG}"
fi
