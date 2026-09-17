#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Fail when TypeScript module paths differ only by letter case or by .ts/.tsx extension"

#USAGE flag "--dir... <dir>" help="A source directory to scan. This flag can be provided multiple times. Defaults to the client app source trees."

# Two files whose paths differ only by letter case, or only by a .ts/.tsx
# extension, are reachable from a single import specifier. The bundler resolves
# that specifier to exactly one of them and the other is never loaded, so the
# build fails with a missing export rather than anything that points at the
# real cause (GRW-127).
#
# The case-only variant is detectable ONLY on a case-sensitive filesystem: git
# cannot lay both files down on a macOS checkout in the first place, so this
# check earns its keep on Linux CI. Do not move it into a pre-commit hook and
# assume it still covers macOS developers.
#
# pipefail matters here. Without it only the final grep/awk status survives the
# pipeline, so a failing `find` would look identical to "no collisions found"
# and the check would pass silently — the worst direction for a guard whose
# whole job is catching what CI cannot otherwise see.
set -euo pipefail

dirs=("${usage_dir[@]:-}")
if [ -z "${dirs[0]}" ]; then
  dirs=(client/admin/src client/dashboard/src)
fi

# Emit a GitHub Actions error annotation alongside human output when running in CI.
gh_error() {
  if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
    echo "::error file=$1::$2"
  fi
}

# Print each group of files sharing a case-insensitive, extension-stripped path,
# one indented path per line, blank line between groups. Groups of one are
# dropped, so empty output means no collisions.
collisions_in() {
  find "$1" -type f \( -name '*.ts' -o -name '*.tsx' \) -print |
    awk '{ key = tolower($0); sub(/\.tsx?$/, "", key); printf "%s\t%s\n", key, $0 }' |
    LC_ALL=C sort |
    awk -F'\t' '
      function flush() { if (n > 1) printf "%s\n", group }
      $1 != key { flush(); key = $1; n = 0; group = "" }
      { n++; group = group "  " $2 "\n" }
      END { flush() }
    '
}

failed=0

for dir in "${dirs[@]}"; do
  echo "🔎 Checking module paths in $dir..."

  collisions="$(collisions_in "$dir")"

  if [ -z "$collisions" ]; then
    echo "✅ $dir"
    continue
  fi

  failed=1
  echo "$collisions"

  while IFS= read -r file; do
    [ -n "$file" ] && gh_error "$file" "Module path is ambiguous: another file differs from it only by letter case or by its .ts/.tsx extension."
  done <<<"$(echo "$collisions" | sed 's/^  //')"
done

if [ "$failed" -eq 0 ]; then
  exit 0
fi

echo "
🚨 The file groups above share a module path.
🚨
🚨 Each group is reachable from one import specifier, so the resolver picks a
🚨 single file and the rest are never loaded:
🚨
🚨   - Paths differing only by letter case collide on case-insensitive
🚨     filesystems, which is the macOS default. Linux CI is the only place this
🚨     can be caught.
🚨   - A .ts and a .tsx file sharing a stem are ambiguous on every filesystem,
🚨     because the resolver tries .ts first.
🚨
🚨 Give one file in each group a distinct stem.
"
exit 1
