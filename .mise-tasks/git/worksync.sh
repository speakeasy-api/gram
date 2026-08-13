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

if [ "${usage_no_migrate:-false}" = "true" ]; then
  echo
  echo "ℹ️  Skipping database migrations (--no-migrate)."
  exit 0
fi

echo
echo "⏳ Applying Postgres migrations..."
mise run db:migrate

echo
echo "⏳ Applying ClickHouse migrations..."
mise run clickhouse:migrate

echo
echo "✅ Worktree synced."
