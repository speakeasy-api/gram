#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Generate from the dev-idp Goa design files (nested under internal/devidp/)"

set -e

exec goa gen github.com/speakeasy-api/gram/server/internal/devidp/design \
  -o internal/devidp
