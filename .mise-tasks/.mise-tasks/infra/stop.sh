#!/usr/bin/env bash
#MISE description="Stop all docker compose services"
#MISE sources=["/dev/null"]

set -e

docker compose --profile "*" down --remove-orphans