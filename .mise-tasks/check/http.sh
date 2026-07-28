#!/usr/bin/env bash

#MISE hide=true
#MISE description="Check that an HTTP service is up and healthy"
#MISE quiet=true
#USAGE flag "--url <url>" required=#true help="The HTTP endpoint to health check"

set -euo pipefail

url=${usage_url:?--url is required}

status="$(curl --silent --show-error --output /dev/null \
  --connect-timeout 2 --max-time 5 \
  --write-out "%{http_code}" "$url")"

if [[ "$status" != "200" ]]; then
  exit 1
fi
