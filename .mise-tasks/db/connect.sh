#!/usr/bin/env bash

#MISE description="Connect to the local development database with psql (or pgcli with --pretty)"

#USAGE flag "--pretty" help="Use pgcli instead of psql"
#USAGE arg "<db_args>..." help="Extra arguments forwarded to psql/pgcli" default=""

set -eo pipefail

if [ "${usage_pretty:-}" = "true" ]; then
  exec pgcli "${GRAM_DATABASE_URL%&search_path=*}" ${usage_db_args:-}
else
  exec psql "${GRAM_DATABASE_URL%&search_path=*}" ${usage_db_args:-}
fi
