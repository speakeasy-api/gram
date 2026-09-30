#!/usr/bin/env bash

#MISE description="Open the Grafana observability UI (traces, metrics, logs)"

set -e

url="http://localhost:${GRAFANA_PORT:?Environment variable GRAFANA_PORT must be set}"

# LGTM is an opt-in shared profile. Opening Grafana is the moment a developer
# asked for it, so start the container if it is not already up.
mise run infra:lgtm

exec mise run open:_thing "$url"
