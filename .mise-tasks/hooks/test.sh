#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin locally"
#MISE dir="{{ config_root }}"

#USAGE flag "--server-url <url>" env="GRAM_SERVER_URL" help="Gram server URL used for hook posting and smoke validation"
#USAGE flag "--api-key <key>" env="GRAM_API_KEY" help="Project-scoped Gram API key with hooks scope"
#USAGE flag "--project-slug <slug>" env="GRAM_PROJECT_SLUG" help="Gram project slug used by the hook upload path"

set -euo pipefail

SERVER_URL="${usage_server_url:-${GRAM_SERVER_URL:-https://localhost:8080}}"
API_KEY="${usage_api_key:-${GRAM_API_KEY:-}}"
PROJECT_SLUG="${usage_project_slug:-${GRAM_PROJECT_SLUG:-${VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG:-ecommerce-api}}}"

: "${DB_USER:?DB_USER must be set}"
: "${DB_NAME:?DB_NAME must be set}"

if [ -z "${SERVER_URL}" ] || [ -z "${API_KEY}" ] || [ -z "${PROJECT_SLUG}" ]; then
  cat >&2 <<EOF
hooks:test requires a project API key.

Defaults used when not provided:
  server-url: https://localhost:8080
  project-slug: ecommerce-api

Provide overrides via flags:
  mise hooks:test --server-url https://localhost:8080 --api-key <hooks-api-key> --project-slug <project-slug>

Or via environment variables:
  export GRAM_SERVER_URL=https://localhost:8080
  export GRAM_API_KEY=<hooks-api-key>
  export GRAM_PROJECT_SLUG=<project-slug>

Current resolved values:
  server-url: ${SERVER_URL:-<missing>}
  api-key: $( [ -n "${API_KEY}" ] && echo '<set>' || echo '<missing>' )
  project-slug: ${PROJECT_SLUG:-<missing>}
EOF
  exit 1
fi

export GRAM_HOOKS_SERVER_URL="${SERVER_URL}"
export GRAM_API_KEY="${API_KEY}"
export GRAM_PROJECT_SLUG="${PROJECT_SLUG}"
export GRAM_SKILLS_UPLOAD_ENABLED=true
export GRAM_HOOKS_DEBUG=true

producer_tests=(
  "hooks/shared-producer/producer-core.test.mts"
  "hooks/shared-producer/upload.test.mts"
  "hooks/shared-producer/cache.test.mts"
)

existing_tests=()
for test_file in "${producer_tests[@]}"; do
  if [ -f "$test_file" ]; then
    existing_tests+=("$test_file")
  fi
done

cleanup() {
  if [ -n "${SMOKE_SKILL_DIR:-}" ] && [ -d "${SMOKE_SKILL_DIR}" ]; then
    rm -rf "${SMOKE_SKILL_DIR}"
    mise run skills:sync >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

if [ "${#existing_tests[@]}" -gt 0 ]; then
  echo "Running shared producer tests..."
  node --test "${existing_tests[@]}"
  echo ""
fi

if [ -n "${CLAUDECODE:-}" ]; then
  echo "Skipping Claude smoke because this task is running inside an existing Claude session."
  echo "Producer tests completed successfully."
  exit 0
fi

if ! command -v claude >/dev/null 2>&1; then
  echo "Error: claude CLI is not installed or not on PATH." >&2
  exit 1
fi

if ! command -v node >/dev/null 2>&1; then
  echo "Error: node is required for the skills-capture plugin smoke test." >&2
  exit 1
fi

if ! curl -skf "${GRAM_HOOKS_SERVER_URL%/}/healthz" >/dev/null; then
  echo "Error: local Gram server is not reachable at ${GRAM_HOOKS_SERVER_URL}." >&2
  exit 1
fi

echo "Validating API key/project context..."
key_validation_json="$(curl -skf \
  -H "Gram-Key: ${GRAM_API_KEY}" \
  "${GRAM_HOOKS_SERVER_URL%/}/rpc/keys.verify" || true)"
if [ -z "${key_validation_json}" ]; then
  echo "Error: failed to validate GRAM_API_KEY against ${GRAM_HOOKS_SERVER_URL}." >&2
  exit 1
fi
if ! printf '%s' "${key_validation_json}" | grep -q '"hooks"'; then
  echo "Error: GRAM_API_KEY does not appear to include hooks scope." >&2
  echo "keys.validate response: ${key_validation_json}" >&2
  exit 1
fi
if ! printf '%s' "${key_validation_json}" | grep -q '"slug":"'"${GRAM_PROJECT_SLUG}"'"'; then
  echo "Error: GRAM_API_KEY does not appear to have access to project slug ${GRAM_PROJECT_SLUG}." >&2
  echo "keys.validate response: ${key_validation_json}" >&2
  exit 1
fi

ORG_ID="$(printf '%s' "${key_validation_json}" | sed -n 's/.*"organization":{"id":"\([^"]*\)".*/\1/p')"
if [ -z "${ORG_ID}" ]; then
  echo "Error: failed to extract organization ID from keys.verify response." >&2
  echo "keys.validate response: ${key_validation_json}" >&2
  exit 1
fi

echo "Ensuring local skills_capture feature flag is enabled for org ${ORG_ID}..."
docker compose exec -T gram-db psql -U "${DB_USER}" -d "${DB_NAME}" <<EOF >/dev/null
INSERT INTO organization_features (organization_id, feature_name)
VALUES ('${ORG_ID}', 'skills_capture')
ON CONFLICT (organization_id, feature_name)
WHERE deleted IS FALSE
DO NOTHING;
EOF

echo "Clearing cached product feature state for skills_capture..."
docker compose exec -T gram-cache redis-cli -p 35299 -a "${GRAM_REDIS_CACHE_PASSWORD:-xi9XILbY}" DEL "feature:${ORG_ID}:skills_capture" >/dev/null || true

echo "Preparing Claude smoke test skill..."
smoke_suffix="$(date +%s)"
SMOKE_SKILL_SLUG="engineer-onboarding-e2e-smoke-${smoke_suffix}"
SMOKE_TOKEN="final-capture-attempt-${smoke_suffix}"
SMOKE_SKILL_DIR=".agents/skills/${SMOKE_SKILL_SLUG}"
mkdir -p "${SMOKE_SKILL_DIR}"
cat >"${SMOKE_SKILL_DIR}/SKILL.md" <<EOF
---
name: ${SMOKE_SKILL_SLUG}
---

# ${SMOKE_SKILL_SLUG}

This is a smoke-test skill for validating automated Claude hook capture.

When invoked, reply with exactly:

${SMOKE_TOKEN}
EOF

mise run skills:sync >/dev/null
rm -f ~/.gram/skills-upload-cache.json ~/.gram/hooks-debug.log

echo "Running Claude non-interactive smoke with skills-capture plugin..."
output_file="$(mktemp)"
if ! env -u CLAUDE_CODE -u CLAUDECODE -u CLAUDE_CODE_SSE_PORT \
  claude --plugin-dir ./hooks/plugin-claude-skills -p "Use the skill ${SMOKE_SKILL_SLUG} and then reply exactly: ${SMOKE_TOKEN}" >"${output_file}"; then
  echo "Claude smoke command failed." >&2
  cat "${output_file}" >&2 || true
  rm -f "${output_file}"
  exit 1
fi

if ! grep -q "${SMOKE_TOKEN}" "${output_file}"; then
  echo "Claude smoke did not return the expected token." >&2
  cat "${output_file}" >&2 || true
  rm -f "${output_file}"
  exit 1
fi
rm -f "${output_file}"

echo "Validating debug-log signals..."
if [ ! -f ~/.gram/hooks-debug.log ]; then
  echo "Expected ~/.gram/hooks-debug.log to exist after Claude smoke." >&2
  exit 1
fi
if ! grep -q 'hooks_post_result' ~/.gram/hooks-debug.log; then
  echo "Expected hooks_post_result in ~/.gram/hooks-debug.log." >&2
  exit 1
fi
if ! grep -q '"firstSkill":{"name":"'"${SMOKE_SKILL_SLUG}"'"' ~/.gram/hooks-debug.log; then
  echo "Claude smoke completed, but no skill invocation for ${SMOKE_SKILL_SLUG} was observed in ~/.gram/hooks-debug.log." >&2
  echo "Recent debug-log excerpt:" >&2
  grep -E 'start|enriched|upload_|hooks_post_' ~/.gram/hooks-debug.log | tail -n 20 >&2 || true
  exit 1
fi
if ! grep -q 'upload_worker_spawn_result' ~/.gram/hooks-debug.log && ! grep -q 'upload_suppressed_recent_duplicate' ~/.gram/hooks-debug.log; then
  echo "Expected upload_worker_spawn_result or upload_suppressed_recent_duplicate in ~/.gram/hooks-debug.log." >&2
  echo "Recent debug-log excerpt:" >&2
  grep -E 'start|enriched|upload_|hooks_post_' ~/.gram/hooks-debug.log | tail -n 20 >&2 || true
  exit 1
fi

echo "Validating backend persistence..."
attempts_table_exists="$(docker compose exec -T gram-db psql -U "${DB_USER}" -d "${DB_NAME}" -Atc "SELECT to_regclass('public.skills_capture_attempts') IS NOT NULL;" | tr -d '[:space:]')"
if [ "${attempts_table_exists}" = "t" ]; then
  latest_outcome="$(docker compose exec -T gram-db psql -U "${DB_USER}" -d "${DB_NAME}" -Atc "SELECT outcome FROM skills_capture_attempts WHERE skill_slug = '${SMOKE_SKILL_SLUG}' AND deleted IS FALSE ORDER BY created_at DESC LIMIT 1;" | tr -d '[:space:]')"
  if [ "${latest_outcome}" != "accepted" ] && [ "${latest_outcome}" != "duplicate" ]; then
    echo "Expected latest skills_capture_attempts outcome to be accepted or duplicate, got '${latest_outcome}'." >&2
    exit 1
  fi
else
  echo "Warning: skills_capture_attempts table is missing in the local DB; skipping attempt-history assertion." >&2
  echo "Run your local migration/reset flow if you want full provenance validation." >&2
fi

skill_count="$(docker compose exec -T gram-db psql -U "${DB_USER}" -d "${DB_NAME}" -Atc "SELECT COUNT(*) FROM skills WHERE slug = '${SMOKE_SKILL_SLUG}' AND deleted IS FALSE;" | tr -d '[:space:]')"
if [ "${skill_count}" = "0" ]; then
  echo "Expected a skills row for ${SMOKE_SKILL_SLUG}." >&2
  exit 1
fi

version_count="$(docker compose exec -T gram-db psql -U "${DB_USER}" -d "${DB_NAME}" -Atc "SELECT COUNT(*) FROM skill_versions sv INNER JOIN skills s ON s.id = sv.skill_id WHERE s.slug = '${SMOKE_SKILL_SLUG}' AND s.deleted IS FALSE;" | tr -d '[:space:]')"
if [ "${version_count}" = "0" ]; then
  echo "Expected at least one skill_versions row for ${SMOKE_SKILL_SLUG}." >&2
  exit 1
fi

echo "Claude hook smoke passed."
