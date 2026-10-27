package remotesessions_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestListRemoteSessions_GatewayLoginOrder(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name               string
		organizationIssuer bool
		organizationClient bool
		gatewayFirst       bool
	}{
		{name: "project_gateway_first", gatewayFirst: true},
		{name: "project_member_first"},
		{name: "organization_gateway_first", organizationIssuer: true, gatewayFirst: true},
		{name: "organization_member_first", organizationIssuer: true},
		{name: "shared_client_gateway_first", organizationIssuer: true, organizationClient: true, gatewayFirst: true},
		{name: "shared_client_member_first", organizationIssuer: true, organizationClient: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const resource = "https://upstream.example.com/mcp"
			options := []syntheticLoginOption{withRemoteServer(resource), withResource(resource)}
			if tc.organizationClient {
				// Organization clients use an issuer without a project of its own.
				options = append(options, withGlobalIssuer())
			}
			ctx, env := newSyntheticExpiryEnv(t, tc.name, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"EXAMPLE-gateway-token","token_type":"Bearer","expires_in":3600}`))
			}, options...)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			ctx = contextvalues.WithValidatedGramSession(ctx, auth, false)
			subject := urn.NewUserSubject(auth.UserID)
			memberIssuer := env.session.UserSessionIssuerID

			var gatewayIssuer uuid.UUID
			var gatewayID uuid.NullUUID
			gatewayRoute := mcproute.MCP
			if tc.organizationIssuer {
				gatewayIssuer = seedOrganizationTierUserSessionIssuer(t, ctx, env.db, "listing-gateway")
				gatewayRoute = mcproute.OrganizationGateway
			} else {
				issuer, err := usersessionsrepo.New(env.db).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
					ProjectID: env.projectID, OrganizationID: conv.ToPGText(env.organizationID),
					Slug: "listing-gateway", AuthnChallengeMode: "interactive",
					SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
				})
				require.NoError(t, err)
				gatewayIssuer = issuer.ID
				gateway, err := metamcprepo.New(env.db).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
					ProjectID: env.projectID, OrganizationID: env.organizationID, Name: "Listing gateway",
					UserSessionIssuerID: conv.ToNullUUID(gatewayIssuer), Visibility: "private",
				})
				require.NoError(t, err)
				gatewayID = conv.ToNullUUID(gateway.ID)
				_, err = metamcprepo.New(env.db).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
					ProjectID: env.projectID, MetaMcpServerID: gateway.ID, McpServerID: env.mcpServerID.UUID,
				})
				require.NoError(t, err)
			}

			clientProject := conv.ToNullUUID(env.projectID)
			if tc.organizationClient {
				clientProject = uuid.NullUUID{}
			}
			client, err := env.q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
				ProjectID: clientProject, OrganizationID: conv.ToPGText(env.organizationID),
				RemoteSessionIssuerID: env.issuerID, ClientID: "listing-client", TokenEndpointAuthMethod: conv.ToPGText("none"),
			})
			require.NoError(t, err)
			for _, issuer := range []uuid.UUID{memberIssuer, gatewayIssuer} {
				require.NoError(t, env.q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
					RemoteSessionClientID: client.ID, UserSessionIssuerID: issuer,
				}))
			}

			authorizer := agentmanagement.NewAuthorizer(bindingOwnerOnlyEngine{})
			env.service.SetBindingAuthorizer(func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
				_, _, err := authorizer.RequireAgentOwnerForUpdate(ctx, tx, id, agentmanagement.OwnedAgentAuthorize)
				if err != nil {
					return fmt.Errorf("authorize attachment owner: %w", err)
				}
				return nil
			})
			agent, err := agentrepo.New(env.db).CreateAgent(ctx, agentrepo.CreateAgentParams{
				OrganizationID: env.organizationID, OwnerUserID: auth.UserID, Name: "Gateway listing agent",
			})
			require.NoError(t, err)

			issuers := []uuid.UUID{memberIssuer, gatewayIssuer}
			if tc.gatewayFirst {
				issuers[0], issuers[1] = issuers[1], issuers[0]
			}
			var sessionID uuid.UUID
			for step, issuer := range issuers {
				var clients []remotesessions.Client
				if tc.organizationIssuer && issuer == gatewayIssuer {
					clients, err = env.mgr.ListOrganizationClients(ctx, env.organizationID, issuer)
				} else {
					clients, err = env.mgr.ListClients(ctx, env.projectID, env.organizationID, issuer)
				}
				require.NoError(t, err)
				var loginClient remotesessions.Client
				for _, candidate := range clients {
					if candidate.ID == client.ID {
						loginClient = candidate
					}
				}
				require.Equal(t, client.ID, loginClient.ID)
				parent := remotesessions.ParentChallenge{
					ID: uuid.NewString(), ProjectID: env.projectID, OrganizationID: env.organizationID,
					UserSessionIssuerID: issuer, Subject: &subject, McpSlug: "listing-server",
					McpServerID: env.mcpServerID, Resource: resource,
				}
				if issuer == gatewayIssuer {
					parent.McpServerID = uuid.NullUUID{}
					parent.ResourceMcpServerID = env.mcpServerID
					parent.MetaMcpServerID = gatewayID
					parent.RouteBase = gatewayRoute
				}
				authorizationURL, err := env.mgr.BuildAuthorizationUrl(ctx, parent, loginClient)
				require.NoError(t, err)
				parsed, err := url.Parse(authorizationURL)
				require.NoError(t, err)
				callback, err := env.callback(t, url.Values{"state": {parsed.Query().Get("state")}, "code": {"EXAMPLE-code"}}.Encode())
				require.NoError(t, err)
				require.Equal(t, http.StatusSeeOther, callback.Code)

				stored, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: client.ID})
				require.NoError(t, err)
				if step == 0 {
					sessionID = stored.ID
				}
				require.Equal(t, sessionID, stored.ID, "reauthorization must reuse the subject/client session")
				require.Equal(t, issuers[0], stored.UserSessionIssuerID, "reauthorization preserves the original issuer")
				require.EqualValues(t, step+1, stored.GrantGeneration)

				for _, payload := range []*gen.ListRemoteSessionsPayload{
					{SubjectUrn: new(subject.String())},
					{RemoteSessionClientID: new(client.ID.String())},
					{SubjectUrn: new(subject.String()), RemoteSessionClientID: new(client.ID.String())},
				} {
					listed, err := env.service.ListRemoteSessions(ctx, payload)
					require.NoError(t, err)
					require.Len(t, listed.Items, 1)
					require.Equal(t, sessionID.String(), listed.Items[0].ID)
					require.Equal(t, issuers[0].String(), listed.Items[0].UserSessionIssuerID)
				}
				count, err := env.service.CountRemoteSessions(ctx, &gen.CountRemoteSessionsPayload{RemoteSessionClientID: client.ID.String()})
				require.NoError(t, err)
				require.EqualValues(t, 1, count.Subjects)

				for _, target := range issuers {
					eligible, err := env.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
						PrincipalID: new(agent.ID.String()), UserSessionIssuerID: new(target.String()),
					})
					require.NoError(t, err)
					if target == issuers[0] {
						require.Len(t, eligible.Items, 1)
						require.Equal(t, sessionID.String(), eligible.Items[0].ID)
					} else {
						require.Empty(t, eligible.Items, "a shared client does not authorize cross-issuer attachment")
					}
				}
			}

			// The bootstrap login and the subject's grant must each appear once
			// when traversing the unfiltered project list.
			var listedIDs []string
			payload := &gen.ListRemoteSessionsPayload{Limit: new(1)}
			for {
				listed, err := env.service.ListRemoteSessions(ctx, payload)
				require.NoError(t, err)
				for _, item := range listed.Items {
					require.NotContains(t, listedIDs, item.ID)
					listedIDs = append(listedIDs, item.ID)
				}
				if listed.NextCursor == nil {
					break
				}
				payload.Cursor = listed.NextCursor
			}
			require.ElementsMatch(t, []string{env.session.ID.String(), sessionID.String()}, listedIDs)

			require.NoError(t, env.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{ID: sessionID.String()}))
			listed, err := env.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{SubjectUrn: new(subject.String())})
			require.NoError(t, err)
			require.Empty(t, listed.Items)
		})
	}
}
