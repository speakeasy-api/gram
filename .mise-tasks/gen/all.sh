#!/usr/bin/env bash

#MISE description="Run all code generation tasks in dependency order"

set -e

mise run gen:server

pids=()
mise run gen:sdk            & pids+=($!)
mise run gen:devidp         & pids+=($!)
mise run gen:posting-server & pids+=($!)

status=0
for pid in "${pids[@]}"; do
  wait "$pid" || status=$?
done
exit $status
