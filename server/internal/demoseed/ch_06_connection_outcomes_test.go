package demoseed

import (
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/stretchr/testify/require"
)

func TestConnectionOutcomeRowsCoverRequestRefreshAndRenewal(t *testing.T) {
	t.Parallel()
	s := &seeder{spec: DefaultSpec(), now: time.Now()}
	rows, err := s.connectionRequestRows()
	require.NoError(t, err)
	require.Len(t, rows, connectionAttemptDays*len(connectionRequestEndings))
	steps := make(map[string]bool)
	outcomes := make(map[string]bool)
	attempts := make(map[string]bool)
	refusalKinds := make(map[string]bool)
	scheduledRefreshes, clientRefreshes := 0, 0
	for _, row := range rows {
		require.True(t, row.IsFinal)
		require.False(t, attempts[row.AttemptID.String()], "each outcome is its own attempt")
		attempts[row.AttemptID.String()] = true
		steps[row.Step] = true
		outcomes[row.Step+"/"+row.Reason] = true
		outcomes[row.Step+"/"+row.StepResult] = true
		if row.Reason != "" {
			meaning, ok := mcpconnectionattempts.MeaningOf(row.Reason)
			require.True(t, ok, row.Reason)
			require.Equal(t, string(meaning.Outcome), row.Outcome)
		} else {
			require.Equal(t, "connected", row.Outcome)
		}
		if row.Step == "mcp_request" || row.Step == "inbound_token" || (row.Step == "provider_refresh" && row.AppKey != "background") {
			require.NotEmpty(t, row.ClientFamily)
			require.NotEmpty(t, row.ClientVersion)
			require.Regexp(t, `^[0-9a-f]{32}$`, row.DeviceKey)
		}
		require.NotEqual(t, "rejected_by_member", row.Reason, "request outcomes must match a live request producer")
		if row.Step == "mcp_request" && (row.Reason == "provider_server_error" || row.Reason == "tool_permission_denied" || row.Reason == "usage_limit_reached" || row.Reason == "provider_rejected" || row.Reason == "mcp_tool_calls_paused") {
			refusalKinds[row.Reason+"/"+row.ServerKind] = true
			require.Equal(t, s.projectID(), row.ProjectID)
			switch row.ServerKind {
			case "meta":
				require.Equal(t, s.det("metamcp-1").String(), row.ServerID)
				require.Equal(t, s.supportServer().String(), row.MemberID)
			case "mcp":
				require.Equal(t, s.det("mcpserver-github").String(), row.ServerID)
				require.Empty(t, row.MemberID, "a direct server has no member")
			default:
				require.Fail(t, "refusal on an unexpected server kind", row.ServerKind)
			}
		} else {
			require.Empty(t, row.MemberID, "no member should be attributed to a different gateway")
		}
		switch row.Step {
		case "inbound_token":
			require.Equal(t, "refresh_token", row.GrantType)
		case "sign_in_renewal":
			require.Equal(t, "identity_provider", row.ServerKind)
			require.Empty(t, row.ClientFamily, "background renewal has no browser")
		case "provider_refresh":
			if row.AppKey == "background" {
				scheduledRefreshes++
				require.Equal(t, "provider_session", row.ServerKind)
				require.Empty(t, row.ClientFamily)
			} else {
				clientRefreshes++
			}
		}
	}
	for _, outcome := range []string{
		"mcp_request/mcp_tool_calls_paused",
		"mcp_request/provider_server_error",
		"mcp_request/invalid_bearer_token",
		"mcp_request/no_server_access",
		"mcp_request/usage_limit_reached",
		"sign_in_renewal/reauthentication_required",
		"sign_in_renewal/renewal_retryable",
		"mcp_request/provider_rejected",
		"mcp_request/tool_permission_denied",
		"inbound_token/refresh_token_expired",
		"inbound_token/refresh_replayed",
		"inbound_token/refresh_token_revoked",
		"inbound_token/refresh_token_used_by_other_app",
		"inbound_token/refresh_token_client_mismatch",
		"inbound_token/stored_session_invalid",
		"inbound_token/sign_in_revoked_by_admin",
		"inbound_token/refreshed",
		"mcp_request/invalid_remote_session",
		"mcp_request/remote_session_unavailable",
		"provider_refresh/refreshed",
		"provider_refresh/invalid_grant",
		"provider_refresh/provider_server_error",
		"sign_in_renewal/renewed",
	} {
		require.True(t, outcomes[outcome], outcome)
	}
	require.Positive(t, scheduledRefreshes, "some refreshes are the scheduled sweep")
	require.Positive(t, clientRefreshes, "some refreshes come from a client")
	for _, reason := range []string{"provider_server_error", "tool_permission_denied", "usage_limit_reached", "provider_rejected", "mcp_tool_calls_paused"} {
		require.True(t, refusalKinds[reason+"/mcp"], reason+" on a direct server")
		require.True(t, refusalKinds[reason+"/meta"], reason+" on a gateway")
	}
	for _, step := range []string{"mcp_request", "inbound_token", "provider_refresh", "sign_in_renewal"} {
		require.True(t, steps[step], step)
	}
}

func TestConnectionOutcomeSeedKeepsAbandonedBrowserIdentity(t *testing.T) {
	t.Parallel()
	s := &seeder{spec: DefaultSpec()}
	attempt := mcpconnectionattempts.AttemptAt(s.det("stopped-attempt"), time.Now(), s.connectionServerScope())
	rows, err := s.connectionAttemptRows(attempt, "stopped-attempt", connectionEndings[0], true)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, mcpconnectionattempts.FamilyBrowser, row.ClientFamily)
		require.Equal(t, s.connectionDevice("stopped-attempt"), row.DeviceKey)
		require.Empty(t, row.MemberID)
		require.False(t, row.IsFinal)
	}
}
