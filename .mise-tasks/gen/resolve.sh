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

paths=(.speakeasy client/dashboard/src/sdk server/gen)

echo "==> Checking out $base for: ${paths[*]}"
git checkout "$base" -- "${paths[@]}"

echo "==> Regenerating Goa server"
mise run gen:goa-server

echo "==> Regenerating SDK"
mise run gen:sdk

echo "==> Done. Review and stage the regenerated files."
