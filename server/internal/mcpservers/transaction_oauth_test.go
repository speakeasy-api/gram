package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	backfill "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCreateMCPServerInTransaction_PublishesOAuthBeforeCommit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := createOrganizationIssuer(t, ctx, ti, auth.ActiveOrganizationID)
	remoteIssuer, remoteClient := linkRemoteClientWithID(t, ctx, ti, auth.ActiveOrganizationID, *auth.ProjectID, issuer)
	source := uuid.New()
	fixtures := backfill.New(ti.conn)
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, backfill.SeedRemoteMCPServerFixtureParams{ID: source, ProjectID: *auth.ProjectID, Url: "https://mcp.example.com/atomic-create"}))
	tx := testenv.BeginTx(t, ctx, ti.conn)
	server, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, tx, audit.NewLogger(), mcpservers.MCPServerTransactionInput{
		OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, ActorUserID: auth.UserID, ActorEmail: auth.Email,
		Name: "Atomic OAuth server", Visibility: mcpservers.VisibilityPrivate,
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.MustParse(issuer), Valid: true},
		RemoteMCPServerID:   uuid.NullUUID{UUID: source, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: remoteIssuer, Valid: true}, server.RemoteSessionIssuerID)
	links, err := backfill.New(tx).ListServerAuthClientsFixture(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{backfill.OAuthClientID(auth.ActiveOrganizationID, remoteClient, backfill.TypeRemoteSession)}, links, "the configured registration must have its stable typed OAuth link before commit")
	require.NoError(t, tx.Rollback(ctx))
	links, err = fixtures.ListServerAuthClientsFixture(ctx, server.ID)
	require.NoError(t, err)
	require.Empty(t, links, "rollback must not publish typed OAuth state")
}

// A server created on a user session issuer another server already signs in
// through gets that server's client only while the issuer's bindings are what
// serves both. Under upstream auth enforcement a server offers what was
// configured for it, and nothing was configured for the new one.
func TestCreateMCPServerInTransaction_InheritsIssuerClientOnlyWithoutEnforcement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enforced bool
	}{
		{name: "enforcement off", enforced: false},
		{name: "enforcement on", enforced: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			if tc.enforced {
				_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
					OrganizationID: auth.ActiveOrganizationID,
					FeatureName:    string(productfeatures.FeatureUpstreamAuthEnforced),
				})
				require.NoError(t, err)
			}
			issuer := createOrganizationIssuer(t, ctx, ti, auth.ActiveOrganizationID)
			fixtures := backfill.New(ti.conn)
			create := func(name, url string) uuid.UUID {
				source := uuid.New()
				require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, backfill.SeedRemoteMCPServerFixtureParams{ID: source, ProjectID: *auth.ProjectID, Url: url}))
				tx := testenv.BeginTx(t, ctx, ti.conn)
				server, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, tx, audit.NewLogger(), mcpservers.MCPServerTransactionInput{
					OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, ActorUserID: auth.UserID, ActorEmail: auth.Email,
					Name: name, Visibility: mcpservers.VisibilityPrivate,
					UserSessionIssuerID: uuid.NullUUID{UUID: uuid.MustParse(issuer), Valid: true},
					RemoteMCPServerID:   uuid.NullUUID{UUID: source, Valid: true},
				})
				require.NoError(t, err)
				require.NoError(t, tx.Commit(ctx))
				return server.ID
			}

			// The first server is the issuer's only one when the client is
			// bound, so the client is its own.
			first := create("Configured server", "https://mcp.example.com/configured")
			_, remoteClient := linkRemoteClientWithID(t, ctx, ti, auth.ActiveOrganizationID, *auth.ProjectID, issuer)
			typedClient := backfill.OAuthClientID(auth.ActiveOrganizationID, remoteClient, backfill.TypeRemoteSession)
			links, err := fixtures.ListServerAuthClientsFixture(ctx, first)
			require.NoError(t, err)
			require.Equal(t, []uuid.UUID{typedClient}, links)

			second := create("Unconfigured server", "https://mcp.example.com/unconfigured")

			links, err = fixtures.ListServerAuthClientsFixture(ctx, second)
			require.NoError(t, err)
			if tc.enforced {
				require.Empty(t, links, "an enforced server offers a client nobody configured for it")
			} else {
				require.Equal(t, []uuid.UUID{typedClient}, links)
			}
			links, err = fixtures.ListServerAuthClientsFixture(ctx, first)
			require.NoError(t, err)
			require.Equal(t, []uuid.UUID{typedClient}, links)

			// Alone on the issuer, the second server is its bindings' only
			// consumer. That proves nothing under enforcement either: nobody
			// configured the first server's client for it.
			require.NoError(t, ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{ID: first.String()}))
			links, err = fixtures.ListServerAuthClientsFixture(ctx, second)
			require.NoError(t, err)
			if tc.enforced {
				require.Empty(t, links, "an enforced server offers the client of a server deleted beside it")
			} else {
				require.Equal(t, []uuid.UUID{typedClient}, links)
			}
		})
	}
}
