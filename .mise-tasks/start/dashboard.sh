#!/usr/bin/env bash
#MISE description="Start up the Gram Dashboard dev server"

set -e

exec pnpm --filter ./client/dashboard dev