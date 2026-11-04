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
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Equal(t, mcpconnectionattempts.FamilyBrowser, row.ClientFamily)
		require.Equal(t, rows[0].DeviceKey, row.DeviceKey)
		require.False(t, row.IsFinal)
	}
}
