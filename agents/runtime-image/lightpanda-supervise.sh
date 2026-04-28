#!/usr/bin/env bash

set -uo pipefail

log=/tmp/lightpanda.log

while true; do
  /usr/local/bin/lightpanda serve --host 127.0.0.1 --port 9222 \
    >>"$log" 2>&1
  ec=$?
  printf '[supervise] lightpanda exited %d, restarting in 1s\n' "$ec" >>"$log"
  sleep 1
done
