#!/usr/bin/env bash

#MISE description="Open the Temporal Web UI"

set -e

url="http://localhost:${TEMPORAL_WEB_PORT:?Environment variable TEMPORAL_WEB_PORT must be set}"

exec mise run open:_thing "$url"