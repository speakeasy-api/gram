package remotesessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestRefreshBinding_IsScopedToSessionIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	_, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	q := repo.New(ti.conn)
	lookup := repo.HasActiveEMABindingForRefreshParams{RemoteSessionClientID: conv.ToNullUUID(in.ClientID), UserSessionIssuerID: in.UserSessionIssuerID}
	active, err := q.HasActiveEMABindingForRefresh(ctx, lookup)
	require.NoError(t, err)
	require.True(t, active)
	// A different identity issuer cannot inherit another binding for this client.
	lookup.UserSessionIssuerID = createUserSessionIssuer(t, ctx, ti.conn, "unbound-refresh")
	active, err = q.HasActiveEMABindingForRefresh(ctx, lookup)
	require.NoError(t, err)
	require.False(t, active)
}
