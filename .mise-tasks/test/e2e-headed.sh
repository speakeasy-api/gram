#!/usr/bin/env bash
#MISE description="Run Playwright E2E tests in headed browser mode"
#MISE alias="e2e-headed"

set -e

exec pnpm --filter ./client/dashboard test:e2e:headed "$@"
