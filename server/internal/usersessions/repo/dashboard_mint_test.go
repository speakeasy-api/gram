package repo

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
)

// The list query matches dashboard mints with a SQL literal. The mint writes
// the same sentinel from DashboardMintRefreshTokenHashPrefix, so the two have
// to stay identical or Connections drops those rows back to an unknown client.
func TestListUserSessionsQueryMatchesDashboardMintPrefix(t *testing.T) {
	t.Parallel()

	needle := "'" + sessiontokens.DashboardMintRefreshTokenHashPrefix + ":%'"
	require.Contains(t, listUserSessionsByProjectID, needle)
}
