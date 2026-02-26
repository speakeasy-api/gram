#!/usr/bin/env bash

#MISE description="Install NPM dependencies"
#MISE hide=true

#USAGE flag "--offline" help="Install using only the local cache"

set -e

args=()
if [[ "${usage_offline:-}" == "true" ]]; then
  args+=(--offline)
fi

exec pnpm i "${args[@]}"
