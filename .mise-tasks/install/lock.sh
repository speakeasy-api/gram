#!/usr/bin/env bash

#MISE description="Change NPM dependencies and rewrite pnpm-lock.yaml with pnpm"

#USAGE flag "-F --filter <package>" help="Workspace package to add to, e.g. admin"
#USAGE flag "-D --dev" help="Add to devDependencies"
#USAGE arg "[package]..." var=#true help="Packages to add; omit to re-resolve the manifests as they stand"

set -euo pipefail

root="${MISE_CONFIG_ROOT:-$PWD}"

# pnpm, not aube, writes this lockfile. aube 1.38.1 and 1.40.0 both rewrite it
# wholesale on every write: they order peer suffixes differently (~1400 lines of
# churn) and resolve `workspace:` specifiers to registry versions, silently
# unlinking local packages. aube reads pnpm's output fine, which is all CI does.
# See `mise run check:lockfile`.
#
# pnpm is run against a copy of the manifests because `pnpm add` links
# node_modules even under --lockfile-only, and this tree's node_modules is
# aube-shaped — pnpm would either clobber it or die on ERR_PNPM_HOIST_PATTERN_DIFF.
mirror="$(mktemp -d)"
trap 'rm -rf "$mirror"' EXIT

cd "$root"
cp pnpm-lock.yaml pnpm-workspace.yaml "$mirror/"
cp -R patches "$mirror/patches"
while IFS= read -r manifest; do
  mkdir -p "$mirror/$(dirname "$manifest")"
  cp "$manifest" "$mirror/$manifest"
done < <(git ls-files '*package.json' ':!:**/node_modules/**')

cd "$mirror"
if [[ -n "${usage_package:-}" ]]; then
  args=(add --lockfile-only)
  [[ -n "${usage_filter:-}" ]] && args+=(--filter "$usage_filter")
  [[ "${usage_dev:-}" == "true" ]] && args+=(--save-dev)
  # usage passes variadic args as a shell-quoted string.
  eval "set -- $usage_package"
  pnpm "${args[@]}" "$@"
else
  pnpm install --lockfile-only --ignore-scripts
fi

cd "$root"
cp "$mirror/pnpm-lock.yaml" pnpm-lock.yaml
while IFS= read -r manifest; do
  cmp -s "$mirror/$manifest" "$manifest" || cp "$mirror/$manifest" "$manifest"
done < <(git ls-files '*package.json' ':!:**/node_modules/**')

# Called by path, not `mise run`: a mise task's PATH can resolve to a different
# mise than the one running it.
bash .mise-tasks/check/lockfile.sh
exec aube install
