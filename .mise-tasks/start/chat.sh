#!/usr/bin/env bash
#MISE description="Start up the Gram Chat dev server"

set -e

# Elements is a dependency of the chat app and must be built first
pnpm --filter ./elements build

exec pnpm --filter ./client/chat dev
