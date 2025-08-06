#!/usr/bin/env bash
#MISE description="Destroy all infra resources"

set -e

docker compose --profile "*" down --volumes --rmi local --remove-orphans

echo ""
echo "💥 All infra resources destroyed"
echo "💥 Run \`./zero\` to get back up and running"