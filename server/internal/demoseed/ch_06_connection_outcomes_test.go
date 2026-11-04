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
	scopes := []mcpconnectionattempts.Scope{
		mcpconnectionattempts.ServerScope(s.orgID(), s.projectID(), s.det("mcpserver-github"), mcpconnectionattempts.AppDCR),
		mcpconnectionattempts.GatewayScope(s.orgID(), s.projectID(), s.det("metamcp-1"), mcpconnectionattempts.AppDCR),
		mcpconnectionattempts.OrganizationGatewayScope(s.orgID(), s.det("organization-gateway"), mcpconnectionattempts.AppDCR),
	}
	rows, err := s.connectionRequestRows(scopes)
	require.NoError(t, err)
	require.Len(t, rows, connectionAttemptDays*len(connectionRequestEndings))
	steps := make(map[string]bool)
	outcomes := make(map[string]bool)
	attempts := make(map[string]bool)
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
		if row.Step == "mcp_request" || row.Step == "inbound_token" {
			require.NotEmpty(t, row.ClientFamily)
			require.NotEmpty(t, row.ClientVersion)
			require.Regexp(t, `^[0-9a-f]{32}$`, row.DeviceKey)
		}
		require.NotEqual(t, "rejected_by_member", row.Reason, "request outcomes must match a live request producer")
		if row.Step == "mcp_request" && (row.Reason == "provider_server_error" || row.Reason == "tool_permission_denied" || row.Reason == "provider_rejected" || row.Reason == "mcp_tool_calls_paused") {
			require.Equal(t, "meta", row.ServerKind)
			require.Equal(t, s.projectID(), row.ProjectID)
			require.Equal(t, s.det("metamcp-1").String(), row.ServerID)
			require.Equal(t, s.det("mcpserver-support").String(), row.MemberID)
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
				require.Equal(t, "provider_session", row.ServerKind)
				require.Empty(t, row.ClientFamily)
			}
		}
	}
	for _, outcome := range []string{
		"mcp_request/mcp_tool_calls_paused",
		"mcp_request/provider_server_error",
		"mcp_request/provider_rejected",
		"mcp_request/tool_permission_denied",
		"inbound_token/refresh_token_expired",
		"inbound_token/refresh_replayed",
		"inbound_token/refresh_token_revoked",
	} {
		require.True(t, outcomes[outcome], outcome)
	}
	for _, step := range []string{"mcp_request", "inbound_token", "provider_refresh", "sign_in_renewal"} {
		require.True(t, steps[step], step)
	}
}

func TestConnectionOutcomeSeedKeepsAbandonedBrowserIdentity(t *testing.T) {
	t.Parallel()
	s := &seeder{spec: DefaultSpec()}
	for _, scope := range []mcpconnectionattempts.Scope{
		mcpconnectionattempts.ServerScope(s.orgID(), s.projectID(), s.det("mcpserver-github"), mcpconnectionattempts.AppDCR),
		mcpconnectionattempts.GatewayScope(s.orgID(), s.projectID(), s.det("metamcp-1"), mcpconnectionattempts.AppDCR),
		mcpconnectionattempts.OrganizationGatewayScope(s.orgID(), s.det("organization-gateway"), mcpconnectionattempts.AppDCR),
	} {
		attempt := mcpconnectionattempts.AttemptAt(s.det("stopped-attempt"), time.Now(), scope)
		rows, err := s.connectionAttemptRows(attempt, "stopped-attempt", connectionEndings[0], true)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, mcpconnectionattempts.FamilyBrowser, rows[0].ClientFamily)
		require.Equal(t, s.connectionDevice("stopped-attempt"), rows[0].DeviceKey)
		require.Empty(t, rows[0].MemberID)
		require.False(t, rows[0].IsFinal)
	}
}
