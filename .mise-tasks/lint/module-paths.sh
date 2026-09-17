#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Fail when TypeScript module paths differ only by letter case or by .ts/.tsx extension"

# Files that match after lowercasing and dropping .ts/.tsx answer to the same
# import specifier, so the resolver loads one and silently ignores the rest
# (GRW-127). Git cannot check out a case-only pair on macOS at all, which makes
# Linux CI the only place they can be caught.
#
# pipefail is load-bearing: without it a failing find is indistinguishable from
# a clean run and this passes silently.
set -euo pipefail

[ $# -gt 0 ] || set -- client/admin/src client/dashboard/src

collisions=$(
  find "$@" -type f \( -name '*.ts' -o -name '*.tsx' \) | LC_ALL=C sort -f |
    awk '{ k = tolower($0); sub(/\.tsx?$/, "", k); n[k]++; key[NR] = k; path[NR] = $0 }
         END { for (i = 1; i <= NR; i++) if (n[key[i]] > 1) print "  " path[i] }'
)

[ -n "$collisions" ] || exit 0

echo "Files sharing a module path, so only one of each group is importable:"
echo "$collisions"
echo "Give one file in each group a distinct stem."
exit 1
