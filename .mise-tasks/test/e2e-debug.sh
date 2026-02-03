#!/usr/bin/env bash
#MISE description="Run Playwright E2E tests in debug mode"
#MISE alias="e2e-debug"

set -e

exec pnpm --filter ./client/dashboard test:e2e:debug "$@"
