#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Run local Platform MCP smoke checks; optional phase: registration or readiness."
#USAGE arg "<phase>" help="Smoke phase: registration, readiness, or all" default="all" {
#USAGE   choices "registration" "readiness" "all"
#USAGE }

set -euo pipefail

phase="${1:-all}"

case "$phase" in
  registration)
    mise exec -- go test ./server/internal/platformmcp -run '^(TestRegistrationStoreCompleteRegistrationConvergesPrivateComponents|TestRegistrationStoreDoesNotCountPendingRegistrationsTowardActiveCap)$' -count=1
    ;;
  readiness)
    mise exec -- go test ./server/internal/platformmcp -run '^(TestRuntimeHandlerRecordsReadyAfterSuccessfulToolsList|TestServiceRevokeConnection|TestServiceGetLifecycleDoesNotExposeCredentials|TestRepairActionsAreBoundedAndStateSpecific|TestReadinessFreshnessIsSeparateFromState)$' -count=1
    ;;
  all)
    mise exec -- go test ./server/internal/plugins -run '^TestPluginsService_PublishProject_PlatformMCPAdmissionTransitions$' -count=1
    mise exec -- go test ./server/internal/platformmcp -run '^(TestOAuthHTTPCompletesChallengeStateHandoff|TestRegistrationStoreCompleteRegistrationConvergesPrivateComponents|TestRegistrationStoreDoesNotCountPendingRegistrationsTowardActiveCap|TestRuntimeHandlerRecordsReadyAfterSuccessfulToolsList|TestServiceRevokeConnection|TestServiceGetLifecycleDoesNotExposeCredentials|TestRepairActionsAreBoundedAndStateSpecific|TestReadinessFreshnessIsSeparateFromState)$' -count=1
    ;;
  *)
    echo "usage: mise run smoke:platform-mcp [registration|readiness]" >&2
    exit 2
    ;;
esac
