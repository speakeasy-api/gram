# Sourced by the Vite dev server start tasks; not a task itself, so it is not
# executable. Call `vite_memory_limits <max old space in MB>` before starting
# Vite.
#
# The settings keep a Vite dev server's memory down. Across the three servers
# they cut what is held after a page load by roughly 30%. They must be in the
# environment before Node starts, so they cannot live in vite.config.ts. Each
# one yields to a value already set, so mise.local.toml can override it.
#
#   - MALLOC_ARENA_MAX caps glibc's per-thread arenas, which Node, Tailwind and
#     lightningcss allocate from.
#   - MIMALLOC_PURGE_DELAY makes Rolldown's allocator hand freed memory back to
#     the OS at once rather than holding on to it.
#   - The thread caps shrink the Rolldown and Tailwind worker pools, which
#     default to 1.5x the core count and each keep their own free memory.
#   - --max-old-space-size makes V8 collect garbage well before its default
#     limit of roughly 4 GB. Each task passes a limit a few times the live
#     heap its server keeps.
vite_memory_limits() {
  export MALLOC_ARENA_MAX="${MALLOC_ARENA_MAX:-2}"
  export MIMALLOC_PURGE_DELAY="${MIMALLOC_PURGE_DELAY:-0}"
  export ROLLDOWN_WORKER_THREADS="${ROLLDOWN_WORKER_THREADS:-4}"
  export ROLLDOWN_MAX_BLOCKING_THREADS="${ROLLDOWN_MAX_BLOCKING_THREADS:-4}"
  export RAYON_NUM_THREADS="${RAYON_NUM_THREADS:-4}"
  # Node accepts both spellings of the flag.
  case " ${NODE_OPTIONS:-} " in
    *" --max-old-space-size="* | *" --max_old_space_size="*) ;;
    *) export NODE_OPTIONS="${NODE_OPTIONS:+$NODE_OPTIONS }--max-old-space-size=$1" ;;
  esac
}
