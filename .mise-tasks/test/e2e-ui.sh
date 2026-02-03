#!/usr/bin/env bash
#MISE description="Run Playwright E2E tests with interactive UI"
#MISE alias="e2e-ui"

set -e

exec pnpm --filter ./client/dashboard test:e2e:ui "$@"
