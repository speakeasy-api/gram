package remotesessions_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestPreparationDetach_OnlyProtectsSelectedUserIssuer(t *testing.T) {
	t.Parallel()
	for _, organization := range []bool{false, true} {
		name := "project"
		if organization {
			name = "organization"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			preparationRecordGrants(t, ctx, ti, in.ClientID, []string{preparationJWTGrant})
			result, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, "ready", result.State)
			otherIssuer := createUserSessionIssuer(t, ctx, ti.conn, "detach-inactive")
			auth, _ := contextvalues.GetAuthContext(ctx)
			servers := make(map[uuid.UUID]uuid.UUID)
			for _, issuerID := range []uuid.UUID{in.UserSessionIssuerID, otherIssuer} {
				require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
					RemoteSessionClientID: in.ClientID, UserSessionIssuerID: issuerID,
				}))
				if organization {
					slug := "detach-" + issuerID.String()
					seedRemoteMCPServerForIssuer(t, ctx, ti, issuerID, slug, "https://mcp.example.com/")
					server, err := mcpserversrepo.New(ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{Slug: conv.ToPGText(slug), ProjectID: *auth.ProjectID})
					require.NoError(t, err)
					servers[issuerID] = server.ID
				}
			}
			detach := func(issuerID uuid.UUID) error {
				if organization {
					return ti.service.RemoveClientFromMcpServer(ctx, &orgclientsgen.RemoveClientFromMcpServerPayload{ClientID: in.ClientID.String(), McpServerID: servers[issuerID].String()})
				}
				_, err := ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{ID: in.ClientID.String(), UserSessionIssuerID: issuerID.String()})
				if err != nil {
					return fmt.Errorf("detach user session issuer: %w", err)
				}
				return nil
			}
			require.NoError(t, detach(otherIssuer), "an active binding on A must not block detaching B")
			require.Zero(t, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, in.ClientID, otherIssuer))
			requireOopsCode(t, detach(in.UserSessionIssuerID), oops.CodeConflict)
			require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, in.ClientID, in.UserSessionIssuerID), "conflict rolls back the tentative removal")
			// Client-wide deletion remains blocked by A after B has detached.
			requireOopsCode(t, ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{ID: in.ClientID.String()}), oops.CodeConflict)
		})
	}
}
