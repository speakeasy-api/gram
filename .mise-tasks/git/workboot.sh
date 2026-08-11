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
if INFRA_READINESS_TIMEOUT=300 ./zero --agent; then
    exit 0
fi

# `mise run seed` is `zero`'s last step and its flakiest: it authenticates
# through dev-idp, and that handshake 307s while a just-restarted server
# settles ("auth.callback did not return a session"). Seeding is also the step
# that lifts the org off the demo gate, so a boot that stops here leaves a
# dashboard that looks broken. Retry the seed alone -- if `zero` failed earlier
# than seeding, these retries fail too and the boot is reported failed anyway.
for delay in 15 30 60; do
    sleep "$delay"
    echo "Retrying seed after a ${delay}s wait..."
    if mise run seed; then
        exit 0
    fi
done

exit 1
