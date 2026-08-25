#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE hide="true"
#MISE alias="gwi"
#MISE description="Initialize a worktree"

#USAGE flag "--source <source>" help="Source worktree to copy from (defaults to main worktree)"

set -e

# Find the source worktree to copy shared files from
if [ -n "${usage_source:-}" ]; then
  main_worktree=$(cd "$usage_source" && pwd)
else
  main_worktree=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)
fi
current_worktree=$(git rev-parse --show-toplevel)

if [ -z "$main_worktree" ] || [ "$main_worktree" = "$current_worktree" ]; then
  echo "Error: this task must be run from a git worktree, not the main working tree."
  exit 1
fi

copy_from_main=(
  ./mise.local.toml
  ./local
  ./.vscode
  ./.cursor
  ./.claude
  ./.mise-tasks
)

for item in "${copy_from_main[@]}"; do
  src="${main_worktree}/${item}"
  [ -e "$src" ] || continue
  if [ -d "$src" ]; then
    tools/rclone copy --metadata --links --create-empty-src-dirs "$src" "$item"
  else
    tools/rclone copyto --metadata --links "$src" "$item"
  fi
done

# Seed the custom linter so lint:server can reuse it when build inputs match.
gcl="${main_worktree}/server/bin/gcl"
if [ -x "$gcl" ]; then
  mkdir -p "${current_worktree}/server/bin"
  tools/rclone copyto --metadata "$gcl" "${current_worktree}/server/bin/gcl"
  if [ -f "${gcl}.fingerprint" ]; then
    tools/rclone copyto --metadata "${gcl}.fingerprint" "${current_worktree}/server/bin/gcl.fingerprint"
  fi
fi

mise trust
if ! mise run install:aube --offline; then
  echo "Offline install failed, falling back to online install..."
  mise run install:aube
fi

copied_compose_project=$(mise set --file mise.local.toml 2>/dev/null \
  | awk '$1 == "COMPOSE_PROJECT_NAME" { print $2 }')
copied_clickhouse_database=$(mise set --file mise.local.toml 2>/dev/null \
  | awk '$1 == "CLICKHOUSE_DATABASE" { print $2 }')

suffix=$(LC_ALL=C tr -dc 'a-z0-9' < /dev/urandom | head -c 4)
compose_project="gram-infra-${suffix}"
mise set --file mise.local.toml "COMPOSE_PROJECT_NAME=${compose_project}"

# Temporal runs once for the whole machine. A namespace per Compose project
# isolates workflow IDs, schedules, and task queues across worktrees.
mise set --file mise.local.toml "TEMPORAL_NAMESPACE=${compose_project}"

# Pub/Sub resource paths include the project ID. Giving each worktree its
# Compose project ID keeps identical topic and subscription IDs isolated when
# every worktree connects to one shared emulator.
mise set --file mise.local.toml "GRAM_GCP_PROJECT_ID=${compose_project}"
# Shared ClickHouse isolates worktrees by database. Preserve a copied explicit
# override, but replace legacy `default` and source-derived namespaces with this
# worktree's namespace.
source_clickhouse_database=$(printf '%s' "$copied_compose_project" \
  | tr '[:upper:]-' '[:lower:]_' \
  | tr -c 'a-z0-9_' '_')
clickhouse_database="$copied_clickhouse_database"
if [ "$clickhouse_database" = "$source_clickhouse_database" ] || [ "$clickhouse_database" = "default" ]; then
  clickhouse_database=
fi
if [ -z "$clickhouse_database" ]; then
  clickhouse_database=$(printf '%s' "$compose_project" \
    | tr '[:upper:]-' '[:lower:]_' \
    | tr -c 'a-z0-9_' '_')
fi
if [[ ! "$clickhouse_database" =~ ^[a-z][a-z0-9_]*$ ]]; then
  echo "Error: generated ClickHouse database '$clickhouse_database' is not a safe identifier." >&2
  exit 1
fi

# Move the declaration after copied base config, then redeclare both dependent
# URLs after it. mise interpolation is precedence- and order-sensitive.
mise unset --file mise.local.toml CLICKHOUSE_DATABASE >/dev/null 2>&1 || true
mise set --file mise.local.toml "CLICKHOUSE_DATABASE=${clickhouse_database}"
generated_clickhouse_urls=0
for key in GRAM_CLICKHOUSE_URL GRAM_CLICKHOUSE_GOMIGRATE_URL; do
  value=$(mise set --file mise.local.toml 2>/dev/null \
    | awk -v key="$key" '$1 == key { print $2 }')
  case "$value" in
    *'{{env.CLICKHOUSE_'*)
      mise unset --file mise.local.toml "$key" >/dev/null 2>&1 || true
      generated_clickhouse_urls=1
      ;;
    "") ;;
    *) continue ;;
  esac
  if [ "$key" = "GRAM_CLICKHOUSE_URL" ]; then
    mise set --file mise.local.toml \
      'GRAM_CLICKHOUSE_URL=clickhouse://{{env.CLICKHOUSE_USERNAME}}:{{env.CLICKHOUSE_PASSWORD}}@{{env.CLICKHOUSE_HOST}}:{{env.CLICKHOUSE_NATIVE_PORT}}/{{env.CLICKHOUSE_DATABASE}}?secure=true&skip_verify=true'
  else
    mise set --file mise.local.toml \
      'GRAM_CLICKHOUSE_GOMIGRATE_URL=clickhouse://{{env.CLICKHOUSE_HOST}}:{{env.CLICKHOUSE_NATIVE_PORT}}?database={{env.CLICKHOUSE_DATABASE}}&username={{env.CLICKHOUSE_USERNAME}}&password={{env.CLICKHOUSE_PASSWORD}}&secure=true&skip_verify=true&x-multi-statement=true'
  fi
done
if [ "$generated_clickhouse_urls" -eq 1 ]; then
  mise unset --file mise.local.toml CLICKHOUSE_HTTP_PORT >/dev/null 2>&1 || true
  mise unset --file mise.local.toml CLICKHOUSE_NATIVE_PORT >/dev/null 2>&1 || true
fi

# Temporal, Pub/Sub, and LGTM are shared across every worktree
# (compose.shared.yml). The namespace and project ID above isolate state; this
# label keeps traces and metrics separate too. The OTel SDK reads it directly,
# so nothing in the Go code has to know.
mise set --file mise.local.toml "OTEL_RESOURCE_ATTRIBUTES=worktree=${compose_project}"

remap=$(mise run zero:remap-ports --format flat --file -)
for line in $remap; do
  key="${line%%=*}"
  # We need to first unset keys so that they are set in the correct order
  mise unset --file mise.local.toml "$key"
  mise set --file mise.local.toml "$line"
done

echo ✅ Updated all port mappings for new worktree

# Ports are randomized, so `wt list`'s URL column can't derive them from the
# branch name. Store the dashboard port as a per-branch var for it to read.
# Best-effort: this is display metadata, and the script runs under `set -e` as a
# blocking pre-start hook, so a failure here must not stop the worktree from
# being set up. stderr is left alone so the reason is still visible.
site_port=$(printf '%s\n' $remap | sed -n 's/^GRAM_SITE_PORT=//p')
if [ -n "$site_port" ] && command -v wt &> /dev/null; then
  wt config state vars set "siteport=${site_port}" > /dev/null || true
fi
