#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Run deterministic Platform MCP smoke checks. Standard local startup needs no separate setup phase."
#USAGE arg "<phase>" help="Smoke phase: read-only, registration, readiness, full, or all" default="read-only" {
#USAGE   choices "read-only" "registration" "readiness" "full" "all"
#USAGE }

set -euo pipefail

phase="${1:-read-only}"

read_only() {
  mise exec -- go test ./server/internal/plugins -run '^TestPluginsService_PublishProject_PlatformMCPAdmissionTransitions$' -count=1
  mise exec -- go test ./server/internal/platformmcp -run '^(TestOAuthHTTPCompletesChallengeStateHandoff|TestRegistrationStoreCompleteRegistrationConvergesPrivateComponents|TestRegistrationStoreDoesNotCountPendingRegistrationsTowardActiveCap|TestRuntimeHandlerRecordsReadyAfterSuccessfulToolsList|TestReadinessToolOutputDoesNotExposeProviderAuthorizationIdentity|TestRepairActionsAreBoundedAndStateSpecific|TestReadinessFreshnessIsSeparateFromState)$' -count=1
}

case "$phase" in
  read-only)
    read_only
    ;;
  registration)
    mise exec -- go test ./server/internal/platformmcp -run '^(TestRegistrationStoreCompleteRegistrationConvergesPrivateComponents|TestRegistrationStoreDoesNotCountPendingRegistrationsTowardActiveCap|TestRegistrationStoreEnforcesActiveRegistrationCap|TestRegistrationStoreSerializesCapRejectionsForDistinctCandidates)$' -count=1
    ;;
  readiness)
    mise exec -- go test ./server/internal/platformmcp -run '^(TestRuntimeHandlerRecordsReadyAfterSuccessfulToolsList|TestReadinessToolOutputDoesNotExposeProviderAuthorizationIdentity|TestRepairActionsAreBoundedAndStateSpecific|TestReadinessFreshnessIsSeparateFromState)$' -count=1
    ;;
  full|all)
    read_only
    mise exec -- go test ./server/internal/platformmcp -run '^(TestRegistrationStoreEnforcesActiveRegistrationCap|TestRegistrationStoreSerializesCapRejectionsForDistinctCandidates)$' -count=1
    ;;
  *)
    echo "usage: mise run smoke:platform-mcp [read-only|registration|readiness|full|all]" >&2
    exit 2
    ;;
esac
