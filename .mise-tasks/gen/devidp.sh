#!/usr/bin/env bash

#MISE dir="{{ config_root }}/dev-idp"
#MISE description="Generate from the dev-idp Goa design files (top-level dev-idp/ project)"

set -e

exec goa gen github.com/speakeasy-api/gram/dev-idp/design -o .
