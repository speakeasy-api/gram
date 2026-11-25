package authclientbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSyncEMABinding_PreservesExactResourceIdentity(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	resource := "https://mcp.example.com/exact"
	source, exactServer := f.emaSourceAt(resource, usi)
	otherSource, otherServer := f.emaSourceAt(resource+"/", usi)
	f.staticHeader(source, "X-Exact-Key")
	f.staticHeader(otherSource, "X-Other-Key")
	registration, issuer := f.emaClient()
	binding := f.binding(usi, issuer, registration, resource, []string{"read"})
	tx := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncEMABinding(t.Context(), tx, f.orgID, binding))
	require.NoError(t, tx.Commit(t.Context()))
	id := EMAClientID(f.orgID, issuer, registration, resource)
	require.Equal(t, []uuid.UUID{id}, f.links(exactServer))
	require.NotContains(t, f.links(otherServer), id, "a trailing slash names a different authorization resource")
	require.Len(t, f.headers(id), 1, "the unrelated source must neither contribute nor suppress credentials")
}
