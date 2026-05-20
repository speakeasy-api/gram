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
