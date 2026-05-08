#!/usr/bin/env bash

#MISE description="Open the Mock IDP dashboard"

set -e

url="http://localhost:${GRAM_DEVIDP_DASHBOARD_PORT:?Environment variable GRAM_DEVIDP_DASHBOARD_PORT must be set}"

exec mise run open:_thing "$url"