#!/usr/bin/env bash

#MISE description="Start the dev-idp operator dashboard"
#MISE hide=true

set -e

if [ -z "${GRAM_DEVIDP_EXTERNAL_URL:-}" ]; then
  echo "GRAM_DEVIDP_EXTERNAL_URL is not set — uncomment in mise.toml or set in mise.local.toml (run \`mise run zero:devidp\` to opt in)." >&2
  exit 1
fi

# Keeps this Vite dev server's memory down. Without these it settles at about 520 MB
# after a page load; with them, about 400 MB. They must be in the environment
# before Node starts, so they cannot live in vite.config.ts. Each one yields to
# a value already set, so mise.local.toml can override it.
#
#   - MALLOC_ARENA_MAX caps glibc's per-thread arenas, which Node, Tailwind and
#     lightningcss allocate from.
#   - MIMALLOC_PURGE_DELAY makes Rolldown's allocator hand freed memory back to
#     the OS at once rather than holding on to it.
#   - The thread caps shrink the Rolldown and Tailwind worker pools, which
#     default to 1.5x the core count and each keep their own free memory.
#   - --max-old-space-size makes V8 collect garbage well before its default
#     limit of roughly 4 GB. This server keeps about 85 MB of live heap.
export MALLOC_ARENA_MAX="${MALLOC_ARENA_MAX:-2}"
export MIMALLOC_PURGE_DELAY="${MIMALLOC_PURGE_DELAY:-0}"
export ROLLDOWN_WORKER_THREADS="${ROLLDOWN_WORKER_THREADS:-4}"
export ROLLDOWN_MAX_BLOCKING_THREADS="${ROLLDOWN_MAX_BLOCKING_THREADS:-4}"
export RAYON_NUM_THREADS="${RAYON_NUM_THREADS:-4}"
case " ${NODE_OPTIONS:-} " in
  *" --max-old-space-size="*) ;;
  *) export NODE_OPTIONS="${NODE_OPTIONS:+$NODE_OPTIONS }--max-old-space-size=640" ;;
esac

exec aube run --filter ./dev-idp-dashboard dev
