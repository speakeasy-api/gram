#!/usr/bin/env bash

#MISE description="Open the dashboard site"

set -e

exec mise run open:_thing "${GRAM_SITE_URL:?Environment variable GRAM_SITE_URL must be set}"