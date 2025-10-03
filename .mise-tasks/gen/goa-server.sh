#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Generate from Goa design files"
#MISE sources=["server/design/**/*.go"]
#MISE outputs=["server/gen/**/*"]

set -e
exec goa gen github.com/speakeasy-api/gram/server/design
