#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE hide="true"
#MISE description="Boot this worktree's stack, publishing a marker while it runs"

set -e

# Runs as the `post-start` hook (see .config/wt.toml). The hook is backgrounded,
# so a fresh worktree spends several minutes with its ports closed while the
# stack comes up. `git:workstatus` can't tell that apart from a stack that was
# never booted, so publish a marker for the duration: the pid of this script,
# in the worktree's own git dir (never committed, removed with the worktree).
# A boot that exits nonzero leaves a second marker behind holding the exit
# code, so the failure survives for `git:workstatus` to report -- otherwise a
# stack that died halfway through is indistinguishable from one never started.
gitdir="$(git rev-parse --absolute-git-dir)"
marker="$gitdir/gram-stack-boot.pid"
failed="$gitdir/gram-stack-boot.failed"

if [ -n "${GRAM_WT_NO_BOOT:-}" ]; then
    echo "GRAM_WT_NO_BOOT set — skipping stack boot. Run ./zero --agent when you need it."
    exit 0
fi

echo $$ > "$marker"
rm -f "$failed"
trap 'code=$?; rm -f "$marker"; [ "$code" -eq 0 ] || echo "$code" > "$failed"' EXIT

# A fresh worktree is booted so that its containers, migrations and seed data
# exist -- but it is not left running: a developer usually has several
# worktrees and only works in one, and idle stacks cost RAM and CPU for nothing.
# So hand the worktree over paused; `mise run wake` brings it back in seconds
# (containers are stopped, not removed, so nothing is re-created or re-seeded).
pause_stack() {
    echo "Boot complete — pausing the stack. Run \`mise run wake\` to use it."
    mise run pause
}

# Re-booting a worktree whose stack is already running fails for a reason that
# has nothing to do with the worktree: `mise run start` kills the previous
# daemons, pitchfork records the kill as a daemon failure, `start` reports
# failure, and `zero` never reaches `mise run seed` -- leaving the org on the
# demo gate. Stop cleanly first so a retry is a real retry. No-op on a fresh
# worktree, where nothing is running yet.
mise run stop || true

# INFRA_READINESS_TIMEOUT is raised from its 30s default because a new worktree
# always starts from a cold volume: Postgres has to finish initdb before it
# accepts queries, and several worktree stacks may be booting at once. At 30s
# infra:start gives up mid-initdb and zero aborts before migrations run,
# leaving the daemons pointed at an empty database.
#
# PRESIDIO_READINESS_TIMEOUT=0 skips the shared analyzer's health wait, which
# is up to 90s of pure latency here: nothing in the boot path consumes Presidio
# synchronously (only background Temporal risk activities do, and they already
# retry), and infra:start treats the wait as advisory anyway. An interactive
# `./zero` keeps the wait so its success message stays honest.
if INFRA_READINESS_TIMEOUT=300 PRESIDIO_READINESS_TIMEOUT=0 ./zero --agent; then
    pause_stack
    exit 0
fi

# `mise run seed` is `zero`'s last step, and the one that fills the org with
# data and lifts it off the demo gate -- a boot that stops here leaves a
# dashboard that looks broken. It talks only to Postgres and ClickHouse (no
# server, no dev-idp handshake), so the remaining failure mode is a database
# still settling on a cold volume. Retry the seed alone -- if `zero` failed
# earlier than seeding, these retries fail too and the boot is reported failed
# anyway.
for delay in 15 30 60; do
    sleep "$delay"
    echo "Retrying seed after a ${delay}s wait..."
    if mise run seed; then
        pause_stack
        exit 0
    fi
done

exit 1
