package mcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// An agent's execute_tool through its gateway to a remote member writes the
// member's tool-call row, attributed to the agent and naming the auth client
// that signed the call in: the owner's attached connection, or a shared
// static credential with no owner. The agent's key names no project, so the
// row is owned by the member server's project.
func TestServeAgentGateway_ExecuteToolLogsTheCredentialUsed(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"off", "enforced"} {
		for _, credential := range []string{authz.AuthClientTypeRemoteSession, authz.AuthClientTypeStaticCredential} {
			t.Run(mode+"/"+credential, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestMCPService(t)
				ti.service.manual = true
				fx := seedAgentGateway(t, ctx, ti)
				ac, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				projectID := *ac.ProjectID
				if mode == "enforced" {
					_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: ac.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
					require.NoError(t, err)
				}
				ti.features.SetFlag(feature.FlagAuthClientResolver, ac.ActiveOrganizationID, false)

				upstream := newRecordingUpstream(t, "ping")
				remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{ProjectID: projectID, TransportType: "streamable-http", Url: upstream.url})
				issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
				serverID := mcpServerBySlug(t, ctx, ti, projectID, fx.granted)
				_, err := mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{ID: serverID, ProjectID: projectID, Name: conv.ToPGText(fx.granted), Slug: conv.ToPGText(fx.granted), RemoteMcpServerID: conv.ToNullUUID(remote.ID), UserSessionIssuerID: conv.ToNullUUID(issuerID), Visibility: mcpservers.VisibilityPrivate})
				require.NoError(t, err)

				wantOwner := ""
				if credential == authz.AuthClientTypeStaticCredential {
					seedRemoteHeader(t, ctx, ti.conn, projectID, remote.ID, "Authorization", "Bearer configured-shared-key", "", true)
					tx := testenv.BeginTx(t, ctx, ti.conn)
					require.NoError(t, authclientbackfill.SyncRemoteSource(ctx, tx, ac.ActiveOrganizationID, remote.ID))
					require.NoError(t, tx.Commit(ctx))
				}
				if credential == authz.AuthClientTypeRemoteSession {
					wantOwner = ac.UserID
					clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, ac.ActiveOrganizationID, "attachment-client", "", []uuid.UUID{issuerID})
					require.NoError(t, upstreamclients.Reconcile(ctx, ti.conn, ac.ActiveOrganizationID, projectID, []uuid.UUID{issuerID}))
					owner := urn.NewUserSubject(ac.UserID)
					insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, owner, "exact-owner-connection", upstream.url)
					q := remotesessionsrepo.New(ti.conn)
					sessions, err := q.ListPrincipalRemoteSessionCandidates(ctx, remotesessionsrepo.ListPrincipalRemoteSessionCandidatesParams{ProjectID: projectID, OrganizationID: ac.ActiveOrganizationID, UserSessionIssuerID: issuerID, SubjectUrn: owner})
					require.NoError(t, err)
					require.Len(t, sessions, 1)
					_, err = q.AttachPrincipalRemoteSessionBinding(ctx, remotesessionsrepo.AttachPrincipalRemoteSessionBindingParams{ProjectID: projectID, OrganizationID: ac.ActiveOrganizationID, PrincipalID: fx.agent.ID, UserSessionIssuerID: issuerID, SubjectUrn: owner.String(), RemoteSessionID: sessions[0].RemoteSession.ID})
					require.NoError(t, err)
				}
				require.NoError(t, upstreamclients.Reconcile(ctx, ti.conn, ac.ActiveOrganizationID, projectID, []uuid.UUID{issuerID}))
				wantClientID := serverAuthClientID(t, ctx, ti, ac.ActiveOrganizationID, serverID, credential)
				fx.token = issueMemberAuthRegressionKey(t, ctx, ti, fx, serverID)

				response, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeMetaRPCBody(t, "tools/call", map[string]any{
					"name":      "execute_tool",
					"arguments": map[string]any{"name": fx.granted + "--ping", "arguments": map[string]any{}},
				}))
				require.NoError(t, err)
				text, isError := metaToolResultText(t, decodeRPCResponse(t, response))
				require.False(t, isError, text)

				requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'tool_call'
		   AND tool_name = 'ping'
		   AND mcp_server_id = ?
		   AND toString(attributes.gram.authorization.actor.type) = 'agent'
		   AND toString(attributes.gram.authorization.actor.id) = ?
		   AND toString(attributes.gram.auth_client.type) = ?
		   AND toString(attributes.gram.auth_client.id) = ?
		   AND toString(attributes.gram.auth_client.connection_owner_user_id) = ?`,
					1, projectID.String(), serverID.String(), fx.agent.ID.String(), credential, wantClientID.String(), wantOwner)
			})
		}
	}
}

// serverAuthClientID is the one enabled auth client of clientType the server
// offers.
func serverAuthClientID(t *testing.T, ctx context.Context, ti *testInstance, orgID string, serverID uuid.UUID, clientType string) uuid.UUID {
	t.Helper()
	rows, err := upstreamclientsrepo.New(ti.conn).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: orgID, McpServerIds: []uuid.UUID{serverID}})
	require.NoError(t, err)
	var ids []uuid.UUID
	for _, row := range rows {
		if row.AuthClientID.Valid && row.ClientType.String == clientType {
			ids = append(ids, row.AuthClientID.UUID)
		}
	}
	require.Len(t, ids, 1)
	return ids[0]
}
