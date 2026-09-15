#!/usr/bin/env bash

#MISE dir="{{ config_root }}/dev-idp"
#MISE description="Start the dev-idp server (OAuth 2.1 authorization server + WorkOS surface)"
#MISE hide=true

set -e

exec go run . "$@"
