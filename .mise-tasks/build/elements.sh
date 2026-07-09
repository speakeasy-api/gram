#!/usr/bin/env bash

#MISE description="Build @gram-ai/elements that powers the dashboard"
#MISE dir="{{ config_root }}/elements"
#MISE depends=["install:pnpm"]
#MISE sources=["src/**/*", "package.json", "tsconfig.json", "vite.config.ts"]
#MISE outputs=["dist/**/*"]

set -e

exec pnpm --filter @gram-ai/elements build
