#!/usr/bin/env bash

#MISE description="Build the internal SDK that powers the dashboard"
#MISE dir="{{ config_root }}/client/sdk"
#MISE depends=["install:pnpm"]

#USAGE flag "--readonly" help="Build with --frozen-lockfile"

set -e

exec pnpm --filter @gram/client build
