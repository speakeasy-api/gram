#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Run the local Platform MCP package, OAuth, discovery, readiness, and revoke smoke checks."

set -euo pipefail

mise exec -- go test ./server/internal/plugins -run '^TestPluginsService_PublishProject_PlatformMCPAdmissionTransitions$' -count=1
mise exec -- go test ./server/internal/platformmcp -run '^(TestOAuthHTTPCompletesChallengeStateHandoff|TestRuntimeHandlerRecordsReadyAfterSuccessfulToolsList|TestServiceRevokeConnection|TestServiceGetLifecycleDoesNotExposeCredentials)$' -count=1
