#!/usr/bin/env bash
#MISE description="Stop this worktree's pitchfork daemons and docker compose services"

set -e

# Stop this worktree's daemons before the containers they depend on go away.
# Otherwise the server, worker and friends keep running against a database that
# no longer exists and fill their logs with connection errors. Best-effort, as
# in nuke: a supervisor that is not running is not an error here, and neither is
# a daemon that has already exited.
if pitchfork supervisor status &> /dev/null; then
    pitchfork stop --all-local || true
fi

# Only this worktree's project. The shared stack (compose.shared.yml) is left
# running on purpose — other worktrees depend on it, and it is cheap to keep up.
# Use `mise run nuke` to tear the shared services down as well.
docker compose --profile "*" down --remove-orphans
