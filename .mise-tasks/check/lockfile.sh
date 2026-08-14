#!/usr/bin/env bash

#MISE description="Check pnpm-lock.yaml still links every workspace dependency"

set -euo pipefail

lockfile="${MISE_CONFIG_ROOT:-.}/pnpm-lock.yaml"

# aube (1.38.1 and 1.40.0) resolves `workspace:` specifiers to the registry
# version instead of `link:` when it writes the lockfile, which swaps a local
# package for a published one without failing any install. Catch that here:
# `aube install --frozen-lockfile` cannot, because an aube-written lockfile is
# self-consistent with every package.json.
broken=$(
  awk '
    /^importers:/ { in_importers = 1; next }
    /^[a-z]/ && !/^importers:/ { in_importers = 0 }
    !in_importers { next }
    /^  [^ ]/ { importer = $1; sub(/:$/, "", importer) }
    /^      [^ ]+:$/ { dep = $1; sub(/:$/, "", dep) }
    /^ +specifier: .?workspace:/ { pending = 1; next }
    /^ +version: / {
      if (pending && $2 !~ /^link:/) print "  - " importer " -> " dep " resolved to " $2
      pending = 0
    }
  ' "$lockfile"
)

if [[ -n "$broken" ]]; then
  echo "❌ Workspace dependencies resolved to registry versions in pnpm-lock.yaml:"
  echo "$broken"
  echo ""
  echo "This lockfile was written by aube. Rewrite it with 'mise run install:lock'."
  exit 1
fi

echo "✅ Every workspace: specifier in pnpm-lock.yaml resolves to a link."
