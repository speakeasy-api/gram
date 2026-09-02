#!/usr/bin/env bash

#MISE description="Resolve merge conflicts in generated artifacts by taking main's version and regenerating"
#MISE dir="{{ config_root }}"

#USAGE flag "-b --base <ref>" help="Git ref to checkout generated artifacts from" default="main"

set -e

base="${usage_base:-main}"

if ! git rev-parse --verify "$base" >/dev/null 2>&1; then
  echo "error: ref '$base' does not exist" >&2
  exit 1
fi

paths=(.speakeasy client/admin/src/sdk client/dashboard/src/sdk server/gen)

echo "==> Checking out $base for: ${paths[*]}"
for path in "${paths[@]}"; do
  if git cat-file -e "$base:$path" 2>/dev/null; then
    git checkout "$base" -- "$path"
  else
    echo "==> $path is absent from $base; keeping it for regeneration"
  fi
done

echo "==> Regenerating Goa server"
mise run gen:goa-server

echo "==> Regenerating SDK"
mise run gen:sdk

echo "==> Done. Review and stage the regenerated files."
