package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"

	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServeConsentMCP_DetachedGitHubAppReplacement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		detach      bool
		replacement bool
	}{
		{name: "detached with static replacement", detach: true, replacement: true},
		{name: "detached without replacement fails closed", detach: true},
		{name: "disabled attached app still fails closed", replacement: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID, orgID := *auth.ProjectID, auth.ActiveOrganizationID
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			member := newValidationMember(t)
			slug := "consent-app-replacement-" + uuid.NewString()
			server, remote := createRemoteMcpEndpoint(t, ctx, ti.conn, projectID, member.url, slug, "public", issuer.ID)
			state, csrf := seedModernConsentChallenge(t, ctx, ti, issuer.ID, client, server.ID, slug)
			endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
			require.NoError(t, err)

			// Keep a valid legacy token available: detaching an App alone must
			// not silently reactivate this credential.
			registration := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "replacement-"+uuid.NewString(), "", []uuid.UUID{issuer.ID})
			insertQualifiedRemoteSessionToken(t, ctx, ti, issuer.ID, registration, urn.NewUserSubject(mockidp.MockUserID), "legacy-token", member.url)
			fixtures := authclientfixtures.New(ti.conn)
			owner := uuid.NullUUID{UUID: projectID, Valid: true}
			appID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: orgID, ProjectID: owner, ClientType: "service_account", Source: "admin"})
			require.NoError(t, err)
			_, err = ti.conn.Exec(ctx, `INSERT INTO mcp_auth_client_github_apps
				(auth_client_id, organization_id, instance_url, app_id, installation_id, private_key_encrypted, upstream_origin)
				VALUES ($1, $2, 'https://git.example.com', 123, 456, 'unused-test-key', $3)`, appID, orgID, member.url)
			require.NoError(t, err)
			require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: projectID, OrganizationID: orgID, McpServerID: server.ID, AuthClientID: appID, Source: "admin"}))
			if tc.detach {
				n, err := authclientsrepo.New(ti.conn).DetachAuthClientFromServer(ctx, authclientsrepo.DetachAuthClientFromServerParams{OrganizationID: orgID, McpServerID: server.ID, AuthClientID: appID})
				require.NoError(t, err)
				require.NotEqual(t, uuid.Nil, n.ID)
				require.Equal(t, projectID, n.ProjectID)
			} else {
				require.NoError(t, fixtures.SetAuthClientEnabledFixture(ctx, authclientfixtures.SetAuthClientEnabledFixtureParams{ID: appID, Enabled: false}))
			}
			if tc.replacement {
				staticID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: orgID, ProjectID: owner, ClientType: "static_credential", Source: "admin"})
				require.NoError(t, err)
				value := pgtype.Text{String: "Bearer replacement-token", Valid: true}
				// Typed and remote header rows match the static identity writer.
				_, err = fixtures.SeedRemoteHeaderFixture(ctx, authclientfixtures.SeedRemoteHeaderFixtureParams{RemoteMcpServerID: remote.ID, Name: "Authorization", Value: value})
				require.NoError(t, err)
				require.NoError(t, fixtures.SeedAuthClientHeaderFixture(ctx, authclientfixtures.SeedAuthClientHeaderFixtureParams{AuthClientID: staticID, OrganizationID: orgID, Name: "Authorization", Value: value}))
				require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: projectID, OrganizationID: orgID, McpServerID: server.ID, AuthClientID: staticID, Source: "admin"}))
			}

			queries := authclientsrepo.New(ti.conn)
			wantAppRequired := !tc.detach || !tc.replacement
			appRequired, err := queries.ServerHasGitHubAppAuthClient(ctx, authclientsrepo.ServerHasGitHubAppAuthClientParams{OrganizationID: orgID, McpServerID: server.ID})
			require.NoError(t, err)
			require.Equal(t, wantAppRequired, appRequired)
			appServers, err := queries.ListGitHubAppServers(ctx, authclientsrepo.ListGitHubAppServersParams{OrganizationID: orgID, McpServerIds: []uuid.UUID{server.ID}})
			require.NoError(t, err)
			if wantAppRequired {
				require.Equal(t, []uuid.UUID{server.ID}, appServers)
			} else {
				require.Empty(t, appServers)
			}

			req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"server/discover"}`)).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Gram-Consent-State", state)
			req.Header.Set("Gram-Consent-Csrf", csrf)
			req.Header.Set("Gram-Consent-Inventory-Attempt", uuid.NewString())
			response := httptest.NewRecorder()
			err = ti.service.ServeConsentMCP(response, req, endpoint)
			requests := member.drain()
			if !tc.detach || !tc.replacement {
				var shared *oops.ShareableError
				require.ErrorAs(t, err, &shared)
				require.Equal(t, http.StatusPreconditionFailed, shared.HTTPStatus(ctx))
				require.Empty(t, requests, "unavailable App identity must not reach the upstream")
				return
			}
			require.NoError(t, err, "a historical App attachment must not reject an explicit replacement with 412")
			require.NotEqual(t, http.StatusPreconditionFailed, response.Code)
			// This member uses the older protocol, so discovery returns 404 and
			// callers negotiate initialize next. Authentication must still arrive.
			require.Equal(t, http.StatusNotFound, response.Code)
			require.Len(t, requests, 1)
			require.Equal(t, "server/discover", requests[0].rpcMethod)
			require.Equal(t, "Bearer replacement-token", requests[0].auth)
		})
	}
}
