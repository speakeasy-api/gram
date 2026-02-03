#!/usr/bin/env bash
#MISE description="Run Playwright E2E tests in headed browser mode (use --auth <path> for authenticated tests)"
#MISE alias="e2e-headed"

set -e

# Parse --auth flag
auth_path=""
args=()

while [[ $# -gt 0 ]]; do
  case $1 in
    --auth)
      auth_path="$2"
      shift 2
      ;;
    --auth=*)
      auth_path="${1#*=}"
      shift
      ;;
    *)
      args+=("$1")
      shift
      ;;
  esac
done

if [[ -n "$auth_path" ]]; then
  export PLAYWRIGHT_AUTH_STATE_PATH="$auth_path"
fi

exec pnpm --filter ./client/dashboard test:e2e:headed "${args[@]}"
