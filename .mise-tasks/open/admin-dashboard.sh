#!/usr/bin/env bash

#MISE description="Open the admin dashboard app"

set -e

exec mise run open:_thing "${GRAM_ADMIN_SERVER_URL:?Environment variable GRAM_ADMIN_SERVER_URL must be set}"
