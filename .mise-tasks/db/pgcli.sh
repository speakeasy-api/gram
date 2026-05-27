#!/usr/bin/env bash

#MISE description="Connect to the local development database with pgcli"

#USAGE arg "<pgcli_args>..." help="Extra arguments forwarded to pgcli" default=""

set -eo pipefail

exec pgcli "${GRAM_DATABASE_URL%&search_path=*}" ${usage_pgcli_args:-}
