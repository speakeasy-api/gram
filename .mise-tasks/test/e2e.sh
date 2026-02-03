#!/usr/bin/env bash
#MISE description="Run Playwright E2E tests for the dashboard"
#MISE alias="e2e"

set -e

exec pnpm --filter ./client/dashboard test:e2e "$@"
