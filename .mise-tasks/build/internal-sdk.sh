#!/usr/bin/env bash

#MISE description="Build the internal SDK that powers the dashboard"
#MISE dir="{{ config_root }}/client/sdk"
#MISE depends=["install:pnpm"]
#MISE sources=["src/**/*", "package.json", "tsconfig.json"]
#MISE outputs=["esm/**/*"]

#USAGE flag "--readonly" help="Build with --frozen-lockfile"

set -e

exec pnpm --filter @gram/client build
