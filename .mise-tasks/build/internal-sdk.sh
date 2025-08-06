#!/usr/bin/env bash

#MISE description="Build the internal SDK that powers the dashboard"
#MISE dir="{{ config_root }}/client/sdk"
#MISE sources=["package.json", "pnpm-lock.yaml", "client/sdk/package.json", "client/sdk/src/**/*.ts"]
#MISE outputs=["client/sdk/esm/**/*"]
#MISE depends=["install:pnpm"]

#USAGE flag "--readonly" help="Build with --frozen-lockfile"

set -e

exec pnpm --filter @gram/client build
