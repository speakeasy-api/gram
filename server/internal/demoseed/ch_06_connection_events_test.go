package demoseed

import (
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/stretchr/testify/require"
)

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
	for name, scope := range map[string]mcpconnectionattempts.Scope{
		"server":               mcpconnectionattempts.ServerScope(s.orgID(), s.projectID(), s.det("mcpserver-github"), mcpconnectionattempts.AppDCR),
		"project gateway":      mcpconnectionattempts.GatewayScope(s.orgID(), s.projectID(), s.det("metamcp-1"), mcpconnectionattempts.AppDCR),
		"organization gateway": mcpconnectionattempts.OrganizationGatewayScope(s.orgID(), s.det("organization-gateway"), mcpconnectionattempts.AppDCR),
	} {
		t.Run(name, func(t *testing.T) {
			attempt := mcpconnectionattempts.AttemptAt(s.det("stopped-attempt"), time.Now(), scope)
			rows, err := s.connectionAttemptRows(attempt, "stopped-attempt", connectionEndings[0], true)
			require.NoError(t, err)
			if name == "project gateway" {
				require.Len(t, rows, 3)
				require.Equal(t, s.det("mcpserver-support").String(), rows[2].MemberID)
				require.Equal(t, mcpconnectionattempts.FamilyBrowser, rows[2].ClientFamily)
				require.Equal(t, s.connectionDevice("stopped-attempt"), rows[2].DeviceKey)
			} else {
				require.Len(t, rows, 2)
				for _, row := range rows {
					require.Empty(t, row.MemberID)
				}
			}
		})
	}
}
