package remotesessions_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestHostedCallbackAuthorizesExactPlacement(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                 string
		stamp                bool
		detachIssuer         bool
		reattach             bool
		detachBeforeExchange bool
		detachBeforeGrant    bool
		wantExchanges        int64
		wantGrant            bool
	}{
		{name: "exact-placement-no-issuer-binding", stamp: true, detachIssuer: true, wantExchanges: 1, wantGrant: true},
		{name: "old-issuer-only-state", stamp: false},
		{name: "placement-removed-before-exchange", stamp: true, detachBeforeExchange: true},
		{name: "placement-replaced-before-exchange", stamp: true, detachBeforeExchange: true, reattach: true},
		{name: "placement-removed-before-grant-write", stamp: true, detachBeforeGrant: true, wantExchanges: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, fx := seedRemoteLoginInFlight(t, tt.name, &revocationSpy{})
			serverID := (attachFixture{ti: fx.ti, projectID: fx.projectID, orgID: fx.organizationID}).hostedServer(t, ctx, "hosted", conv.ToNullUUID(fx.userIssuer))
			associations := upstreamclientsrepo.New(fx.ti.conn)
			added, err := associations.AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
				OrganizationID: fx.organizationID, ProjectID: fx.projectID, McpServerID: serverID, RemoteSessionClientID: fx.clientID,
			})
			require.NoError(t, err)
			require.EqualValues(t, 1, added)

			clients, err := fx.mgr.ListClients(ctx, fx.projectID, fx.organizationID, fx.userIssuer)
			require.NoError(t, err)
			require.Len(t, clients, 1)
			parent := remotesessions.ParentChallenge{
				ID: "hosted-parent", ProjectID: fx.projectID, OrganizationID: fx.organizationID, UserSessionIssuerID: fx.userIssuer,
				Subject: &fx.subject, McpSlug: "hosted", RouteBase: mcproute.MCP, McpServerID: conv.ToNullUUID(serverID), ResourceMcpServerID: conv.ToNullUUID(serverID),
			}
			if tt.stamp {
				parent.HostedPlacement = &remotesessions.HostedPlacement{OrganizationID: fx.organizationID, ProjectID: fx.projectID, ServerID: serverID, RemoteSessionClientID: fx.clientID}
			}
			clients[0].AuthorizationEndpoint += "?resource=https://wrong.example/mcp"
			authorizationURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, clients[0])
			require.NoError(t, err)
			target, err := url.Parse(authorizationURL)
			require.NoError(t, err)
			if tt.stamp {
				require.False(t, target.Query().Has("resource"))
			}
			fx.state = target.Query().Get("state")

			detach := func(ctx context.Context) error {
				_, err := associations.DetachMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.DetachMCPServerRemoteSessionClientParams{
					McpServerID: serverID, ProjectID: fx.projectID, RemoteSessionClientID: fx.clientID,
				})
				if err != nil {
					return fmt.Errorf("detach hosted assignment: %w", err)
				}
				return nil
			}
			if tt.detachIssuer {
				removed, err := repo.New(fx.ti.conn).DetachProjectRemoteSessionClientFromUserSessionIssuer(ctx, repo.DetachProjectRemoteSessionClientFromUserSessionIssuerParams{
					OrganizationID: fx.organizationID, ProjectID: fx.projectID, RemoteSessionClientID: fx.clientID, UserSessionIssuerID: fx.userIssuer,
				})
				require.NoError(t, err)
				require.EqualValues(t, 1, removed)
			}
			if tt.detachBeforeExchange {
				require.NoError(t, detach(ctx))
				if tt.reattach {
					_, err := associations.AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
						OrganizationID: fx.organizationID, ProjectID: fx.projectID, McpServerID: serverID, RemoteSessionClientID: fx.clientID,
					})
					require.NoError(t, err)
				}
			}
			removedBeforeGrant := false
			fx.mgr.SetTypedAuthorizationValidator(func(ctx context.Context, _ remotesessions.RemoteLoginState) error {
				if tt.detachBeforeGrant && fx.exchanges.Load() > 0 && !removedBeforeGrant {
					// This is the subject check after the post-exchange placement
					// read. Only the transactional recheck can catch this detach.
					removedBeforeGrant = true
					return detach(ctx)
				}
				return nil
			})

			callbackErr := fx.mgr.HandleRemoteLoginCallback(httptest.NewRecorder(), callbackRequest(ctx, fx, "hosted-code"))
			require.Equal(t, tt.wantExchanges, fx.exchanges.Load())
			require.Equal(t, tt.detachBeforeGrant, removedBeforeGrant)
			grant, err := repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: fx.clientID})
			if tt.wantGrant {
				require.NoError(t, callbackErr)
				require.NoError(t, err)
				require.Empty(t, grant.Resource.String)
				return
			}
			require.ErrorIs(t, callbackErr, remotesessions.ErrTypedAuthorizationChanged)
			require.ErrorIs(t, err, pgx.ErrNoRows)
		})
	}
}
