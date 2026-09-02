#!/usr/bin/env bash

#MISE description="Wake this worktree's stack if it is paused, then return"
#MISE dir="{{ config_root }}"
#MISE hide=true

set -e

# A dependency of the tasks that cannot work without a running stack (seed,
# playwright, open:dashboard). A worktree is handed over paused, so without
# this those tasks fail with a connection error that says nothing about the
# fix. Cheap enough to depend on unconditionally: one TCP probe when the stack
# is already up.
#
# Deliberately NOT a dependency of build/lint/test/type-check tasks — the point
# of pausing is that most work needs no stack at all.

# Postgres is the load-bearing dependency for everything that depends on this,
# and the one thing `pause` stops that no parker replaces -- so it is the
# probe. bash's /dev/tcp keeps this to zero subprocesses on the hot path, and a
# refused connection to a local port fails immediately.
if exec 3<> "/dev/tcp/${DB_HOST:-127.0.0.1}/${DB_PORT:-5439}" 2> /dev/null; then
    exec 3>&-
    exit 0
fi

# Probe first, guard second: `zero` runs `mise run seed` while the boot marker
# is still held, and that seed must not be turned away. Past this point the
# stack is genuinely down, so a boot in flight means waiting is the only sane
# move -- racing it with a wake fights over the same containers and ports.
gitdir="$(git rev-parse --absolute-git-dir 2>/dev/null || true)"
if [ -n "$gitdir" ] && [ -f "$gitdir/gram-stack-boot.pid" ]; then
    pid=$(cat "$gitdir/gram-stack-boot.pid")
    if ps -o command= -p "$pid" 2> /dev/null | grep -q workboot; then
        echo "Stack is still booting — watch it with \`mise run git:workstatus\`." >&2
        exit 1
    fi
fi

echo "Stack is not running — waking it first."
exec mise run wake
