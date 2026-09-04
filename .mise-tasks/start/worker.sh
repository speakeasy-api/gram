#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Start up the Temporal worker"
#MISE hide=true

set -e

# Temporal schedules are hosted by the shared server, not by this process. If
# the local worker stops, leaving them enabled keeps starting workflows that
# nobody can consume (including a few schedules with seconds-scale cadence).
# Keep their lifecycle attached to the worker as well as to pause/wake so a
# direct `pitchfork stop worker`, a restart, or a crash also becomes idle.
pause_schedules() {
    if ! mise run temporal:schedules --state pause; then
        echo "⚠️  Some Temporal schedules kept running after the worker stopped." >&2
    fi
}
trap pause_schedules EXIT

if ! mise run temporal:schedules --state unpause; then
    echo "⚠️  Some Temporal schedules remain paused while the worker starts." >&2
fi

GIT_SHA=$(git rev-parse HEAD)
go run -ldflags="-X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=${GIT_SHA} -X goa.design/clue/health.Version=${GIT_SHA}" main.go worker
