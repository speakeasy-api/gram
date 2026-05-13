#!/usr/bin/env bash

#MISE description="Open the Jaeger OTEL UI"

set -e

url="http://localhost:${JAEGER_WEB_PORT:?Environment variable JAEGER_WEB_PORT must be set}"

exec mise run open:_thing "$url"