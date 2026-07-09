#!/usr/bin/env bash

#MISE description="Build the static Gram Dashboard Storybook"

set -e

exec pnpm --filter ./client/dashboard build-storybook
