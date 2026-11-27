#!/usr/bin/env bash

#MISE description="Run golangci-lint on the server codebase"
#MISE dir="{{ config_root }}/server"

#USAGE flag "--long" help="Enable more detailed reporting"

# The server is linted with gcl, a golangci-lint binary that has the glint
# plugin compiled in and the patches in server/gcl/patches applied (build:gcl
# builds it). Its cache is shared across worktrees because golangci-lint
# 2.13.2 and later key entries on module-relative paths.

set -eo pipefail

# A cold-cache run keeps every dependency's analysis facts in memory at once
# (about 10 GiB live heap for the server), and with Go's default GOGC the heap
# grows to more than twice that before the collector runs, which pushes even a
# 36 GB laptop into memory compression. A soft limit makes the collector run
# before the heap doubles: it cut a cold run's peak footprint from 27 GiB to
# 11 GiB and made the run faster. Warm runs never get near it. Set GOMEMLIMIT
# in mise.local.toml to override it.
export GOMEMLIMIT="${GOMEMLIMIT:-4GiB}"

args=(--show-stats=false --output.text.print-issued-lines=false)
if [ "${usage_long:-false}" = "true" ]; then
    args=()
fi

mise run build:gcl
gcl_fingerprint=$(cat ./bin/gcl.fingerprint)

# golangci-lint keys its cache on a hash of the plugin directory, but that
# hash does not descend into subdirectories, so analyzer changes under
# glint/imports would otherwise be served stale results. Each gcl build
# therefore gets its own cache directory, named after its inputs (glint and
# the patches among them). Worktrees with the same inputs share one warm
# cache, and a worktree that builds a different gcl does not wipe the cache the
# others use. A new worktree used
# to clear the shared cache on its first build, and the next run anywhere then
# rebuilt every dependency's facts from source (7 minutes, 27 GiB footprint,
# against 9 seconds warm).
gcl_cache_root="${GOLANGCI_LINT_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/gcl}"
export GOLANGCI_LINT_CACHE="$gcl_cache_root/glint-${gcl_fingerprint:0:12}"

# Every run touches its cache directory, so one that no worktree has linted
# with for two weeks belongs to a gcl nobody builds any more. Reads alone
# would not update its modification time.
mkdir -p "$GOLANGCI_LINT_CACHE"
touch "$GOLANGCI_LINT_CACHE"
find "$gcl_cache_root" -mindepth 1 -maxdepth 1 -type d -name 'glint-*' -mtime +14 -exec rm -rf {} +

# Before per-build directories, gcl wrote its cache straight into the default
# root. Nothing reads those entries now, so remove them. Only the default root
# is cleaned: a GOLANGCI_LINT_CACHE set by hand may be shared with stock
# golangci-lint, whose entries use the same layout.
if [ "$gcl_cache_root" = "${XDG_CACHE_HOME:-$HOME/.cache}/gcl" ]; then
    find "$gcl_cache_root" -mindepth 1 -maxdepth 1 -type d -name '[0-9a-f][0-9a-f]' -exec rm -rf {} +
    rm -f "$gcl_cache_root/README" "$gcl_cache_root/trim.txt"
fi

# Both CI lint jobs read the same list, so a local run analyses exactly the
# packages CI does.
lint_package_list=$(mise run -q go:server-lint-packages)
lint_packages=()
while IFS= read -r lint_package; do
    lint_packages+=("$lint_package")
done <<< "$lint_package_list"

exec ./bin/gcl run --max-issues-per-linter=0 "${args[@]}" "${lint_packages[@]}"
