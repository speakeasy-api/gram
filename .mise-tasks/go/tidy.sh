#!/usr/bin/env bash
#MISE description="Run go mod tidy in all Go modules"

set -e

mods=$(find . -name node_modules -prune -o -name go.mod -print)

for mod in $mods; do
  dir=$(dirname "$mod")
  echo "$dir"
  (cd "$dir" && go mod tidy)
done
