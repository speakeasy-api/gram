#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Generate from SQLC files"
#MISE sources=["**/*.sql","database/sqlc.yaml"]
#MISE outputs=["internal/**/{db.go,models.go,queries.sql.go,query.sql.go}"]

set -e
exec sqlc generate -f ./database/sqlc.yaml
