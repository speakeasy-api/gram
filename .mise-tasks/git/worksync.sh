#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE alias="gws"
#MISE description="Sync an existing worktree with main: re-runs port remapping (preserving assigned ports, adding new dependents) and applies pending database migrations. Safe to run repeatedly."

#USAGE flag "--no-migrate" help="Skip applying database migrations."

set -e

main_worktree=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)
current_worktree=$(git rev-parse --show-toplevel)

if [ -z "$main_worktree" ] || [ "$main_worktree" = "$current_worktree" ]; then
  echo "Error: this task must be run from a git worktree, not the main working tree."
  exit 1
fi

if [ ! -f "mise.local.toml" ]; then
  echo "Error: mise.local.toml not found. Initialize this worktree first with 'mise gwi'."
  exit 1
fi

# GRAM_ADMIN_SERVER_URL is now the browser-facing admin origin, meaning the
# admin dashboard dev server, not the admin API. A worktree initialised before
# that still carries a generated declaration pinned to the API port, and
# --preserve below would keep it, leaving the OIDC redirect pointing at an
# origin that serves no SPA. Clear it, plus the origin allowlist derived from
# it, so the remap pass re-emits both against GRAM_ADMIN_DASHBOARD_PORT.
#
# As with PRESIDIO below, the `{{env.GRAM_ADMIN_PORT}}` template is the proof
# the pair was machine generated: only `zero:remap-ports` writes that literal,
# copied verbatim from the old mise.toml value. A hand-pinned admin URL lacks
# the marker and is left entirely alone. The allowlist carries its own marker,
# the `{{env.GRAM_ADMIN_SERVER_URL}}` template, so a hand-written allowlist
# survives even when the URL beside it is cleared.
if grep -E '^GRAM_ADMIN_SERVER_URL[[:space:]]*=' mise.local.toml \
     | grep -qF '{{env.GRAM_ADMIN_PORT}}'; then
  mise unset --file mise.local.toml GRAM_ADMIN_SERVER_URL
  if grep -E '^GRAM_ADMIN_ALLOWED_ORIGINS[[:space:]]*=' mise.local.toml \
       | grep -qF '{{env.GRAM_ADMIN_SERVER_URL}}'; then
    mise unset --file mise.local.toml GRAM_ADMIN_ALLOWED_ORIGINS
  fi
  echo "✅ Cleared the stale admin origin declaration(s); re-mapped below."
fi

# A short-lived dev-idp setup stored the WorkOS API key under the downstream
# GRAM_IDP_CLIENT_SECRET name. Its documented sk_* shape distinguishes it from
# a generated dev-idp secret. Move it to WORKOS_API_KEY when needed, then mint a
# fresh downstream credential. OIDC_CLIENT_SECRET belongs to the retired OIDC
# application and is never treated as a WorkOS API key.
generate_idp_client_secret=false
case "${GRAM_IDP_CLIENT_SECRET:-}" in
  ""|unset)
    generate_idp_client_secret=true
    ;;
  sk_*)
    case "${WORKOS_API_KEY:-}" in
      ""|unset)
        printf '%s' "${GRAM_IDP_CLIENT_SECRET}" \
          | mise set --file mise.local.toml --stdin WORKOS_API_KEY >/dev/null
        echo "✅ Moved the WorkOS API key to WORKOS_API_KEY."
        ;;
    esac
    if grep -qE '^GRAM_IDP_CLIENT_SECRET[[:space:]]*=' mise.local.toml; then
      mise unset --file mise.local.toml GRAM_IDP_CLIENT_SECRET
    fi
    generate_idp_client_secret=true
    ;;
esac

if [ "$generate_idp_client_secret" = "true" ]; then
  idp_client_secret="devidp_$(openssl rand -hex 32)"
  printf '%s' "$idp_client_secret" \
    | mise set --file mise.local.toml --stdin GRAM_IDP_CLIENT_SECRET >/dev/null
  unset idp_client_secret
  echo "✅ Generated a dev-idp client secret for Gram callers."
fi
unset generate_idp_client_secret

if grep -qE '^OIDC_CLIENT_SECRET[[:space:]]*=' mise.local.toml; then
  mise unset --file mise.local.toml OIDC_CLIENT_SECRET
  echo "✅ Removed the retired OIDC client secret; it is not a WorkOS API key."
fi

# dev-idp folded its two identity modes into one WorkOS surface picked by
# GRAM_DEVIDP_BACKEND. Preserve the old WorkOS choice when the replacement has
# not been explicitly configured, then remove the retired setting. The
# generated GRAM_IDP_BASE_URL and WORKOS_API_URL declarations from before the
# prefix rename are refreshed by the remap pass below: they carry the old
# template verbatim, which is how it tells them from hand-pinned values.
if grep -qE '^GRAM_IDP_MODE[[:space:]]*=' mise.local.toml; then
  if grep -qE "^GRAM_IDP_MODE[[:space:]]*=[[:space:]]*['\"]workos['\"][[:space:]]*(#.*)?$" mise.local.toml \
     && ! grep -qE '^GRAM_DEVIDP_BACKEND[[:space:]]*=' mise.local.toml; then
    mise set --file mise.local.toml GRAM_DEVIDP_BACKEND=workos
    echo "✅ Migrated this worktree's identity backend setting to workos."
  fi
  mise unset --file mise.local.toml GRAM_IDP_MODE
  echo "✅ Removed the retired GRAM_IDP_MODE setting."
fi

# Older WorkOS setup wrote a hosted AuthKit client id into the worktree. That
# bypasses dev-idp's fixed login client and restores an interactive external
# login. Remove only the recognizable WorkOS client_* value; custom local
# client ids remain untouched.
if grep -qE "^GRAM_IDP_CLIENT_ID[[:space:]]*=[[:space:]]*['\"]client_[^'\"]*['\"][[:space:]]*(#.*)?$" mise.local.toml; then
  mise unset --file mise.local.toml GRAM_IDP_CLIENT_ID
  echo "✅ Removed the stale WorkOS GRAM_IDP_CLIENT_ID override; using gram-local-dev."
fi

# Fill in per-developer local config added to the main worktree since this one
# was created. Existing copies here are left alone — see git:worklocal.
mise run git:worklocal

echo "⏳ Syncing port mappings..."
added=0
remap=$(mise run zero:remap-ports --preserve --format flat --file -)
for line in $remap; do
  if [ -z "$line" ]; then continue; fi
  key="${line%%=*}"
  mise set --file mise.local.toml "$line"
  echo "  + ${key}"
  added=$((added + 1))
done

if [ "$added" -eq 0 ]; then
  echo "✅ Port mappings already in sync."
else
  echo "✅ Added ${added} env var declaration(s) to mise.local.toml."
fi

# Pub/Sub now uses one emulator on its default port. A generated host declaration
# contains the port template copied from mise.toml, which proves the host and
# port were emitted together by zero:remap-ports. Reset only that generated pair;
# explicit endpoints remain untouched.
if grep -E '^PUBSUB_EMULATOR_HOST[[:space:]]*=' mise.local.toml \
     | grep -qF '{{env.PUBSUB_EMULATOR_PORT}}'; then
  for key in PUBSUB_EMULATOR_HOST PUBSUB_EMULATOR_PORT; do
    if grep -qE "^${key}[[:space:]]*=" mise.local.toml; then
      mise unset --file mise.local.toml "$key"
    fi
  done
  echo "✅ Reset auto-generated Pub/Sub emulator endpoint to the shared default."
fi

# Presidio moved to the shared stack (compose.shared.yml) and must use the
# default port so every worktree reaches the single shared copy. A pre-existing
# worktree may still carry the old auto-generated remap for it, which we reset
# to the mise.toml defaults here — but only when we can prove it was machine
# generated, so a worktree that never remapped (or was hand-edited) is left
# entirely alone.
#
# The proof is the `{{env.PRESIDIO_PORT}}` template in PRESIDIO_ANALYZER_URL:
# `zero:remap-ports` is the only thing that writes that literal
# (`http://127.0.0.1:{{env.PRESIDIO_PORT}}`, copied verbatim from mise.toml), and
# it always emitted PRESIDIO_PORT in the same pass. So the marker attests the
# whole pair is generated, and both are reset together. If a developer has
# deliberately pinned a custom analyzer (their own URL, without the template),
# the marker is absent and neither key is touched. The one case this does not
# distinguish is a hand-set PRESIDIO_PORT left beside the generated template URL;
# that is reset too — acceptable, since keeping the generated URL alongside a
# custom port is not a coherent configuration.
if grep -E '^PRESIDIO_ANALYZER_URL[[:space:]]*=' mise.local.toml \
     | grep -qF '{{env.PRESIDIO_PORT}}'; then
  for key in PRESIDIO_ANALYZER_URL PRESIDIO_PORT; do
    if grep -qE "^${key}[[:space:]]*=" mise.local.toml; then
      mise unset --file mise.local.toml "$key"
    fi
  done
  echo "✅ Reset auto-generated PRESIDIO_PORT / PRESIDIO_ANALYZER_URL to the shared defaults."
fi

# The LGTM observability stack moved to the shared stack for the same reason,
# and needs the same treatment: a pre-existing worktree still carries the
# auto-generated remaps for Grafana/Tempo/Loki/Prometheus and the OTLP
# receivers, which now point at ports nothing is listening on. Same proof as
# above — `zero:remap-ports` is the only thing that writes the
# `{{env.OTLP_GRPC_PORT}}` template into OTEL_EXPORTER_OTLP_ENDPOINT, and it
# emitted the whole group in one pass, so the marker attests the group is
# generated and the group is reset together.
if grep -E '^OTEL_EXPORTER_OTLP_ENDPOINT[[:space:]]*=' mise.local.toml \
     | grep -qF '{{env.OTLP_GRPC_PORT}}'; then
  for key in OTEL_EXPORTER_OTLP_ENDPOINT OTLP_GRPC_PORT OTLP_HTTP_PORT \
             GRAFANA_PORT TEMPO_HTTP_PORT LOKI_HTTP_PORT PROMETHEUS_PORT; do
    if grep -qE "^${key}[[:space:]]*=" mise.local.toml; then
      mise unset --file mise.local.toml "$key"
    fi
  done
  echo "✅ Reset auto-generated LGTM ports to the shared defaults."
fi

# Temporal now runs in the shared stack. Old worktrees have generated remaps for
# both published ports and TEMPORAL_ADDRESS; reset those to the fixed shared
# endpoint. The address template proves the values came from zero:remap-ports,
# so explicit custom Temporal endpoints remain untouched.
if grep -E '^TEMPORAL_ADDRESS[[:space:]]*=' mise.local.toml \
     | grep -qF '{{env.TEMPORAL_PORT}}'; then
  for key in TEMPORAL_ADDRESS TEMPORAL_PORT TEMPORAL_WEB_PORT; do
    if grep -qE "^${key}[[:space:]]*=" mise.local.toml; then
      mise unset --file mise.local.toml "$key"
    fi
  done
  echo "✅ Reset auto-generated Temporal ports to the shared defaults."
fi

# Shared singleton services need a worktree dimension. `git:workinit` writes
# all three values for new worktrees; add them here for older worktrees. Preserve
# custom configuration except Temporal's old `default` value: sharing that
# namespace across worktrees defeats the isolation this migration establishes.
worktree_project=$(mise set --file mise.local.toml 2>/dev/null \
  | awk '$1 == "COMPOSE_PROJECT_NAME" { print $2 }')
if [ -n "$worktree_project" ]; then
  # Workflow IDs, schedules, and task queues are namespace-scoped in Temporal.
  if ! grep -qE '^TEMPORAL_NAMESPACE[[:space:]]*=' mise.local.toml \
     || grep -qE '^TEMPORAL_NAMESPACE[[:space:]]*=[[:space:]]*"default"[[:space:]]*$' mise.local.toml; then
    mise set --file mise.local.toml "TEMPORAL_NAMESPACE=${worktree_project}"
    echo "✅ Namespaced this worktree's Temporal state under ${worktree_project}."
  fi

  # Pub/Sub resource paths include the project ID, so this isolates identical
  # topic and subscription IDs inside the shared emulator.
  if ! grep -qE '^GRAM_GCP_PROJECT_ID[[:space:]]*=' mise.local.toml; then
    mise set --file mise.local.toml "GRAM_GCP_PROJECT_ID=${worktree_project}"
    echo "✅ Namespaced this worktree's Pub/Sub resources under ${worktree_project}."
  fi

  # Without this label, telemetry from same-commit worktrees is
  # indistinguishable in the shared LGTM stack.
  if ! grep -qE '^OTEL_RESOURCE_ATTRIBUTES[[:space:]]*=' mise.local.toml; then
    mise set --file mise.local.toml \
      "OTEL_RESOURCE_ATTRIBUTES=worktree=${worktree_project}"
    echo "✅ Tagged this worktree's telemetry as worktree=${worktree_project}."
  fi
fi

if [ "${usage_no_migrate:-false}" = "true" ]; then
  echo
  echo "ℹ️  Skipping database migrations (--no-migrate)."
  exit 0
fi

echo
# Temporary support for dev-idp schema changes introduced on 2026-09-07.
# Remove after 2026-10-15, when local environments can be assumed evolved.
echo "⏳ Evolving dev-idp SQLite schema..."
mise run db:devidp:evolve

echo
echo "⏳ Applying Postgres migrations..."
mise run db:migrate

echo
echo "⏳ Applying ClickHouse migrations..."
mise run clickhouse:migrate

echo
echo "✅ Worktree synced."
