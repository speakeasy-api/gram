#!/usr/bin/env bash
#MISE description="Stop the local MCP registry"

set -e

docker compose --profile local-registry down --remove-orphans
