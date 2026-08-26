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

# Re-booting a worktree whose stack is already running can make pitchfork treat
# intentional replacement of old daemons as a failure. Stop cleanly first so a
# retry is a real retry. No-op on a fresh worktree.
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
INFRA_READINESS_TIMEOUT=300 PRESIDIO_READINESS_TIMEOUT=0 ./zero --agent
