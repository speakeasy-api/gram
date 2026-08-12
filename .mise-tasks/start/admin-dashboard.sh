#!/usr/bin/env bash

#MISE description="Start up the Gram Admin dashboard dev server"
#MISE hide=true

set -e

exec pnpm --filter ./client/admin dev
