#!/usr/bin/env bash
#MISE description="Run go mod tidy in all Go modules"

set -e

mods="server cli functions"

for mod in $mods; do
  echo "$mod"
  (cd "$mod" && go mod tidy)
done
