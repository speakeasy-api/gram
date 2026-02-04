#!/usr/bin/env bash
#MISE description="Start up the Gram Dashboard dev server"

set -e

# Elements is a dependency of the dashboard and must be built first
pnpm --filter ./elements build

exec pnpm --filter ./client/dashboard dev