#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Generate from SQLC files"

set -e
exec sqlc generate -f ./database/sqlc.yaml
