#!/usr/bin/env bash

#MISE description="Remove all local assistant runtime containers and their workspace volumes"
#MISE confirm="Remove all local assistant runtime containers and workspace volumes?"

set -euo pipefail

filter="label=gram.speakeasy.com/role=assistant_runtime"

containers="$(docker ps --all --quiet --filter "$filter")"
if [ -n "$containers" ]; then
  echo "Removing containers:"
  # shellcheck disable=SC2086
  docker rm --force $containers
else
  echo "No local assistant runtime containers found."
fi

volumes="$(docker volume ls --format '{{.Name}}' | grep '^gram-asst-work-' || true)"
if [ -n "$volumes" ]; then
  echo "Removing workspace volumes:"
  # shellcheck disable=SC2086
  docker volume rm $volumes
else
  echo "No workspace volumes found."
fi

echo ""
echo "Note: the corresponding assistant_runtimes rows will self-heal — the next"
echo "turn re-admits and relaunches a fresh container."
