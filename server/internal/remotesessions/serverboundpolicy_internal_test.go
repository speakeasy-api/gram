// decideServerBound is the whole of AIM-577's per-server rule: which client a
// server's identity commit keeps, replaces, and releases, decided by that
// server's own provider rather than by every provider its user session issuer
// binds for the other servers sharing it.

package remotesessions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestDecideServerBound(t *testing.T) {
	t.Parallel()

	linear := uuid.New()
	notion := uuid.New()
	okta := uuid.New()
	linearClient := repo.RemoteSessionClient{ID: uuid.New(), RemoteSessionIssuerID: linear}
	notionClient := repo.RemoteSessionClient{ID: uuid.New(), RemoteSessionIssuerID: notion}
	spare := repo.RemoteSessionClient{ID: uuid.New(), RemoteSessionIssuerID: linear}
	bound := []repo.RemoteSessionClient{linearClient, notionClient}

	ids := func(clients []repo.RemoteSessionClient) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(clients))
		for _, client := range clients {
			out = append(out, client.ID)
		}
		return out
	}

	t.Run("a sibling's provider is neither reused nor released", func(t *testing.T) {
		t.Parallel()

		// The issuer binds Notion's client for another server. Configuring
		// Okta here must leave it alone, which the superseded per-issuer rule
		// did not: it read two providers as ambiguous.
		decision, err := decideServerBound(bound, clientRegister, okta, uuid.Nil, conv.ToNullUUID(linear))
		require.NoError(t, err)
		require.Nil(t, decision.reuse)
		require.Empty(t, decision.superseded)
		require.Equal(t, []uuid.UUID{linearClient.ID}, ids(decision.release))
	})

	t.Run("an automatic setup reuses the provider's bound client", func(t *testing.T) {
		t.Parallel()

		// A user session issuer holds one client per provider, so a second
		// server signing in to Notion binds the client that is already there
		// instead of registering another.
		decision, err := decideServerBound(bound, clientRegister, notion, uuid.Nil, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
		require.NoError(t, err)
		require.NotNil(t, decision.reuse)
		require.Equal(t, notionClient.ID, decision.reuse.ID)
		require.Empty(t, decision.superseded)
		require.Empty(t, decision.release)
	})

	t.Run("a client of the plan's own supersedes the bound one", func(t *testing.T) {
		t.Parallel()

		for _, kind := range []clientChoiceKind{clientManual, clientLink} {
			decision, err := decideServerBound(bound, kind, linear, uuid.Nil, conv.ToNullUUID(linear))
			require.NoError(t, err)
			require.Nil(t, decision.reuse)
			require.Equal(t, []uuid.UUID{linearClient.ID}, ids(decision.superseded))
			require.Empty(t, decision.release, "the provider keeps a client, so nothing is released")
		}
	})

	t.Run("linking the bound client again touches nothing", func(t *testing.T) {
		t.Parallel()

		decision, err := decideServerBound(bound, clientLink, linear, linearClient.ID, conv.ToNullUUID(linear))
		require.NoError(t, err)
		require.Nil(t, decision.reuse)
		require.Empty(t, decision.superseded)
		require.Empty(t, decision.release)
	})

	t.Run("a created provider has no bound client to reuse", func(t *testing.T) {
		t.Parallel()

		decision, err := decideServerBound(bound, clientRegister, uuid.Nil, uuid.Nil, conv.ToNullUUID(notion))
		require.NoError(t, err)
		require.Nil(t, decision.reuse)
		require.Empty(t, decision.superseded)
		require.Equal(t, []uuid.UUID{notionClient.ID}, ids(decision.release))
	})

	t.Run("a server with no provider releases nothing", func(t *testing.T) {
		t.Parallel()

		decision, err := decideServerBound(bound, clientRegister, okta, uuid.Nil, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
		require.NoError(t, err)
		require.Nil(t, decision.reuse)
		require.Empty(t, decision.superseded)
		require.Empty(t, decision.release)
	})

	t.Run("two clients for one provider are an invariant violation", func(t *testing.T) {
		t.Parallel()

		_, err := decideServerBound([]repo.RemoteSessionClient{linearClient, spare}, clientRegister, linear, uuid.Nil, conv.ToNullUUID(linear))
		require.ErrorIs(t, err, ErrIdentityInvariant)
	})
}
