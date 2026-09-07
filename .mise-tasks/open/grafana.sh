#!/usr/bin/env bash

#MISE description="Open the Grafana observability UI (traces, metrics, logs)"

set -e

url="http://localhost:${GRAFANA_PORT:?Environment variable GRAFANA_PORT must be set}"

exec mise run open:_thing "$url"
