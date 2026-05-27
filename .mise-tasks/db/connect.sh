#!/usr/bin/env bash

#MISE description="Connect to the local development database with psql"

#USAGE arg "<psql_args>..." help="Extra arguments forwarded to psql" default=""

set -eo pipefail

exec psql "${GRAM_DATABASE_URL%&search_path=*}" ${usage_psql_args:-}
