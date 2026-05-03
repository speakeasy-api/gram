#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Generate sqlc Go code for the dev-idp database"

set -e

exec sqlc generate -f ./internal/devidp/database/sqlc.yaml
