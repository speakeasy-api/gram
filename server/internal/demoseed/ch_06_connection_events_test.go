package demoseed

import (
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/stretchr/testify/require"
)

func TestGenericConnectionEndingsMatchSeededApp(t *testing.T) {
	t.Parallel()
	for _, ending := range connectionEndings {
		require.NotEqual(t, mcpconnectionattempts.ResultComplete, ending.what, "CIMD app does not complete at first-party consent")
		require.NotEqual(t, mcpconnectionattempts.CodeReviewCanceled, ending.what, "CIMD app has no Code Mode review")
	}
}

func TestStoppedConnectionAttemptRowsKeepBrowserIdentity(t *testing.T) {
	t.Parallel()
	s := &seeder{spec: DefaultSpec()}
	scope := mcpconnectionattempts.ServerScope(s.orgID(), s.projectID(), s.det("mcpserver-github"), mcpconnectionattempts.AppDCR)
	attempt := mcpconnectionattempts.AttemptAt(s.det("stopped-attempt"), time.Now(), scope)
	rows, err := s.connectionAttemptRows(attempt, "stopped-attempt", connectionEndings[0], true)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, mcpconnectionattempts.FamilyBrowser, row.ClientFamily)
		require.Equal(t, s.connectionDevice("stopped-attempt"), row.DeviceKey)
		require.False(t, row.IsFinal)
	}
}

func TestStoppedConnectionAttemptMembersBelongToGateway(t *testing.T) {
	t.Parallel()
	s := &seeder{spec: DefaultSpec()}
	for name, tc := range map[string]struct {
		scope  mcpconnectionattempts.Scope
		rows   int
		member bool
	}{
		"server":               {mcpconnectionattempts.ServerScope(s.orgID(), s.projectID(), s.det("mcpserver-github"), mcpconnectionattempts.AppDCR), 2, false},
		"project gateway":      {mcpconnectionattempts.GatewayScope(s.orgID(), s.projectID(), s.det("metamcp-1"), mcpconnectionattempts.AppDCR), 3, true},
		"organization gateway": {mcpconnectionattempts.OrganizationGatewayScope(s.orgID(), s.det("organization-gateway"), mcpconnectionattempts.AppDCR), 2, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			attempt := mcpconnectionattempts.AttemptAt(s.det("stopped-attempt"), time.Now(), tc.scope)
			rows, err := s.connectionAttemptRows(attempt, "stopped-attempt", connectionEndings[0], true)
			require.NoError(t, err)
			require.Len(t, rows, tc.rows)
			if tc.member {
				require.Equal(t, s.det("mcpserver-linear").String(), rows[2].MemberID)
				require.Equal(t, mcpconnectionattempts.FamilyBrowser, rows[2].ClientFamily)
				require.Equal(t, s.connectionDevice("stopped-attempt"), rows[2].DeviceKey)
			} else {
				for _, row := range rows {
					require.Empty(t, row.MemberID)
				}
			}
		})
	}
}
