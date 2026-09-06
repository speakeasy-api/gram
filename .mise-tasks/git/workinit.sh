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

mise trust
if ! mise run install:aube --offline; then
  echo "Offline install failed, falling back to online install..."
  mise run install:aube
fi

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
