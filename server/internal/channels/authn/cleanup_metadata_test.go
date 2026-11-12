package authn

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/channels/authn/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/channels/authn/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

func TestCleanupMetadataClients(t *testing.T) {
	t.Parallel()
	container, clone, err := testenv.NewTestPostgres(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })

	fixture := func(t *testing.T) *oauthFixture {
		t.Helper()
		db, err := clone(t, "channels_oauth_cleanup")
		require.NoError(t, err)
		return newOAuthFixture(t, db)
	}
	upsert := func(t *testing.T, f *oauthFixture, id string) {
		t.Helper()
		require.NoError(t, repo.New(f.db).UpsertMetadataClient(t.Context(), repo.UpsertMetadataClientParams{
			ClientID: id, ClientName: "Metadata client", RedirectUris: []string{testRedirect},
		}))
	}
	exists := func(t *testing.T, f *oauthFixture, id string, want bool) {
		t.Helper()
		got, err := fixturerepo.New(f.db).ClientExists(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, want, got, id)
	}

	t.Run("removes unused clients but retains references and revocation tombstones", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		unused := "https://client.example.com/unused.json"
		revoked := "https://client.example.com/revoked.json"
		challenged := "https://client.example.com/challenged.json"
		granted := "https://client.example.com/granted.json"
		for _, id := range []string{unused, revoked, challenged, granted} {
			upsert(t, f, id)
		}
		unusedDCR := f.register(t)
		f.service.clients = fakeMetadataResolver{document: &cimd.Document{
			ClientID: challenged, ClientName: "Metadata client", RedirectURIs: []string{testRedirect},
		}}
		f.challenge(t, challenged)
		f.service.clients = fakeMetadataResolver{document: &cimd.Document{
			ClientID: granted, ClientName: "Metadata client", RedirectURIs: []string{testRedirect},
		}}
		f.authorize(t, granted)
		require.NoError(t, fixturerepo.New(f.db).AgeClients(t.Context()))
		require.NoError(t, fixturerepo.New(f.db).RevokeClient(t.Context(), revoked))
		require.NoError(t, f.service.Cleanup(t.Context()))
		exists(t, f, unused, false)
		exists(t, f, unusedDCR, false)
		for _, id := range []string{revoked, challenged, granted} {
			exists(t, f, id, true)
		}
		// Metadata refresh must not resurrect a tombstone or make it collectible.
		upsert(t, f, revoked)
		require.NoError(t, f.service.Cleanup(t.Context()))
		isRevoked, err := fixturerepo.New(f.db).ClientRevoked(t.Context(), revoked)
		require.NoError(t, err)
		require.True(t, isRevoked)
	})

	t.Run("metadata refresh restarts the unused retention period", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		client := "https://client.example.com/refreshed.json"
		upsert(t, f, client)
		require.NoError(t, fixturerepo.New(f.db).AgeClients(t.Context()))
		upsert(t, f, client)
		require.NoError(t, f.service.Cleanup(t.Context()))
		exists(t, f, client, true)
		require.NoError(t, fixturerepo.New(f.db).AgeClients(t.Context()))
		require.NoError(t, f.service.Cleanup(t.Context()))
		exists(t, f, client, false)
	})

	t.Run("metadata client sweep is bounded", func(t *testing.T) {
		t.Parallel()
		f := fixture(t)
		for i := range 105 {
			upsert(t, f, fmt.Sprintf("https://client.example.com/unused-%d.json", i))
		}
		require.NoError(t, fixturerepo.New(f.db).AgeClients(t.Context()))
		require.NoError(t, f.service.Cleanup(t.Context()))
		remaining, err := fixturerepo.New(f.db).CountRows(t.Context(), "channels_oauth_clients")
		require.NoError(t, err)
		require.EqualValues(t, 5, remaining)
		require.NoError(t, f.service.Cleanup(t.Context()))
		remaining, err = fixturerepo.New(f.db).CountRows(t.Context(), "channels_oauth_clients")
		require.NoError(t, err)
		require.Zero(t, remaining)
	})
}
