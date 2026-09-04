// The refresh grant omits resource only when the issuer is known to reject it.

package remotesessions_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestResolveAccessToken_RefreshResourceFollowsIssuerResourceIndicatorSupport(t *testing.T) {
	t.Parallel()

	const resource = "https://mcp.example.com/mcp"
	cases := []struct {
		name         string
		supported    pgtype.Bool
		wantResource bool
	}{
		{name: "unlearned issuer still sends resource", supported: pgtype.Bool{Bool: false, Valid: false}, wantResource: true},
		{name: "issuer known to accept resource sends it", supported: pgtype.Bool{Bool: true, Valid: true}, wantResource: true},
		{name: "issuer known to reject resource omits it", supported: pgtype.Bool{Bool: false, Valid: true}, wantResource: false},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var spy upstreamSpy
			ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "resource-indicator-"+string(rune('a'+i)), pgtype.Text{String: "", Valid: false}, spyRefreshHandler(&spy))

			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			q := repo.New(ti.conn)
			client, err := q.GetRemoteSessionClientWithIssuerByID(ctx, clientID)
			require.NoError(t, err)
			_, err = q.UpdateRemoteSessionIssuer(ctx, repo.UpdateRemoteSessionIssuerParams{
				ResourceIndicatorSupported: tc.supported,
				ID:                         client.RemoteSessionIssuerID,
				ProjectID:                  conv.ToNullUUID(*authCtx.ProjectID),
			})
			require.NoError(t, err)

			tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, resource)
			require.NoError(t, err)
			require.NoError(t, spy.handlerErr)
			require.Equal(t, "refreshed-access", tok)

			if tc.wantResource {
				require.Equal(t, resource, spy.form.Get("resource"))
			} else {
				require.False(t, spy.form.Has("resource"), "the refresh grant must not send resource to an issuer that rejects it")
			}
		})
	}
}
