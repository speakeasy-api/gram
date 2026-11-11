#!/usr/bin/env bash

#MISE description="Run all code generation tasks in dependency order"

set -e

# The Goa design reads authz scope names, so the authz model's output comes first.
mise run gen:authz
mise run gen:server

pids=()
mise run gen:sdk            & pids+=($!)
mise run gen:devidp         & pids+=($!)
mise run gen:posting-server & pids+=($!)
mise run gen:celwasm        & pids+=($!)

status=0
for pid in "${pids[@]}"; do
  wait "$pid" || status=$?
done
exit $status
