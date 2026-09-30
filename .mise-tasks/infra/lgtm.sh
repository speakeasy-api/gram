#!/usr/bin/env bash

#MISE description="Start the optional shared LGTM observability stack (Grafana, Tempo, Prometheus)"
#MISE dir="{{ config_root }}"

set -euo pipefail

# Large image, unused by most local work. Opt in here or with
# `mise run infra:start --lgtm`. `mise run open:grafana` starts it on demand.
docker compose -f compose.shared.yml -p gram-shared --profile lgtm up -d lgtm
