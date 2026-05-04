#!/usr/bin/env bash

#MISE dir="{{ config_root }}/dev-idp"
#MISE description="Start the dev-idp server (local-speakeasy + oauth2 + oauth2-1 + workos modes)"

set -e

exec go run . "$@"
