#!/usr/bin/env bash
#MISE description="Stop all docker compose services"

set -e

docker compose --profile "*" down --remove-orphans