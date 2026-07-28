#!/usr/bin/env bash

#MISE description="Show local assistant runtime containers, their state, published ports and images"

set -euo pipefail

filter="label=gram.speakeasy.com/role=assistant_runtime"

echo "Local assistant runtime containers:"
docker ps --all --filter "$filter" \
  --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}\t{{.Image}}"

echo ""
echo "Workspace volumes:"
docker volume ls --format "{{.Name}}" | grep '^gram-asst-work-' || echo "(none)"
