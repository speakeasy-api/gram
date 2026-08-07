package activities

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestRemoteSessionRefreshProviderKey(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"https://provider.example.com:8443",
		remoteSessionRefreshProviderKey(pgtype.Text{
			String: "https://Provider.Example.com:8443/oauth/token",
			Valid:  true,
		}),
	)
	require.Empty(t, remoteSessionRefreshProviderKey(pgtype.Text{String: "not a URL", Valid: true}))
	require.Empty(t, remoteSessionRefreshProviderKey(pgtype.Text{}))
}
