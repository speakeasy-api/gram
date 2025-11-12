#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Generate from Goa design files"

set -e
exec goa gen github.com/speakeasy-api/gram/server/design
