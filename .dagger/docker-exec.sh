#!/usr/bin/env bash

# Run Docker-dependent commands with a disposable daemon in the same container.
# Melange and the image checks use bind mounts, so client and daemon must share
# a filesystem. No host Docker socket or persistent daemon state is needed.
set -euo pipefail

dockerd --host=unix:///var/run/docker.sock --storage-driver=vfs > /tmp/dockerd.log 2>&1 &
daemon_pid=$!
trap 'kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true' EXIT

ready=false
for ((attempt=0; attempt<60; attempt++)); do
  if docker info >/dev/null 2>&1; then
    ready=true
    break
  fi
  if ! kill -0 "$daemon_pid" 2>/dev/null; then break; fi
  sleep 1
done
if [ "$ready" != true ]; then
  cat /tmp/dockerd.log >&2
  exit 1
fi

"$@"
