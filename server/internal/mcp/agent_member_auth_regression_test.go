package mcp_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agentsgen "github.com/speakeasy-api/gram/server/gen/agents"
	authclientsgen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	keysgen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/keys"
	"github.com/speakeasy-api/gram/server/internal/mcp/fixtures/fixturerepo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Direct repository fixtures, not browser consent or assistant-turn issuance.
// This finite catalog drives HTTP handlers and a real upstream HTTP server.
// A missing or revoked attachment deliberately denies rather than choosing
// another user's valid connection; under enforcement an agent key then falls
// back only to its validated authorizer's own connection, under the agent's
// own mcp:auth. Same-source reconnect follows the current
// credentials by design. No request-time migration runs.
// Auth permissions and the credential ceiling are explicitly approved through
// management APIs, scoped to this member and its configured auth clients.
func TestAgentMemberAuthRegression(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"off", "shadow", "enforced"} {
		for _, state := range []string{"live", "missing_attachment", "revoked_attachment", "same_source_new_generation", "replaced_source_without_reattach", "disabled_agent", "removed_agent_grant", "owner_block", "credential_block", "noauth", "shared_static_client", "deleted_client", "disabled_typed_client", "shared_oauth_registration", "sibling_issuer_client", "legacy_static_client"} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestMCPService(t)
				ti.service.manual = true
				fx := seedAgentGateway(t, ctx, ti)
				ac, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				projectID := *ac.ProjectID
				// Persist both enabled and explicitly disabled enforcement states.
				features := productfeaturesrepo.New(ti.conn)
				_, err := features.EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: ac.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
				require.NoError(t, err)
				if mode != "enforced" {
					_, err = features.DeleteFeature(ctx, productfeaturesrepo.DeleteFeatureParams{OrganizationID: ac.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
					require.NoError(t, err)
				}
				ti.features.SetFlag(feature.FlagAuthClientResolver, ac.ActiveOrganizationID, false)
				ti.features.SetFlag(feature.FlagAuthClientResolverShadow, ac.ActiveOrganizationID, mode == "shadow")

				upstream := newRecordingUpstream(t, "ping")
				target, err := url.Parse(upstream.url)
				require.NoError(t, err)
				proxy := httputil.NewSingleHostReverseProxy(target)
				var attempts atomic.Int64
				var rejected atomic.Int64
				authenticated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost {
						attempts.Add(1)
						token := r.Header.Get("Authorization")
						if state == "legacy_static_client" && r.Header.Get("X-API-Key") != "configured-legacy-key" {
							rejected.Add(1)
							http.Error(w, "invalid legacy credential", http.StatusUnauthorized)
							return
						}
						if (state == "shared_static_client" || state == "disabled_typed_client") && token != "Bearer configured-shared-key" {
							rejected.Add(1)
							http.Error(w, "invalid shared credential", http.StatusUnauthorized)
							return
						}
						// Under enforcement a replaced source without reattachment is
						// served by the authorizer's own, replacement connection.
						authorizerReplacement := state == "replaced_source_without_reattach" && mode == "enforced" && token == "Bearer replacement-source-token"
						if state != "noauth" && state != "shared_static_client" && state != "disabled_typed_client" && state != "legacy_static_client" && token != "Bearer exact-owner-connection" && token != "Bearer reconnected-owner-connection" && !authorizerReplacement {
							rejected.Add(1)
							http.Error(w, "invalid upstream credential", http.StatusUnauthorized)
							return
						}
					}
					proxy.ServeHTTP(w, r)
				}))
				t.Cleanup(authenticated.Close)
				upstream.url = authenticated.URL
				remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{ProjectID: projectID, TransportType: "streamable-http", Url: upstream.url})
				issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
				serverID := mcpServerBySlug(t, ctx, ti, projectID, fx.granted)
				_, err = mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{ID: serverID, ProjectID: projectID, Name: conv.ToPGText(fx.granted), Slug: conv.ToPGText(fx.granted), RemoteMcpServerID: conv.ToNullUUID(remote.ID), UserSessionIssuerID: conv.ToNullUUID(issuerID), Visibility: mcpservers.VisibilityPrivate})
				require.NoError(t, err)
				if state == "legacy_static_client" {
					// Mark the API key as a credential, not ordinary public metadata.
					_, err := remotemcp.NewHeaders(testenv.NewLogger(t), ti.conn, ti.enc).CreateServerHeader(ctx, remotemcprepo.CreateServerHeaderParams{
						RemoteMcpServerID: remote.ID, ProjectID: projectID, Name: "X-API-Key", IsRequired: true, IsSecret: true, Value: conv.ToPGText("configured-legacy-key"),
					})
					require.NoError(t, err)
				}
				var clientID uuid.UUID
				if state == "shared_static_client" || state == "disabled_typed_client" {
					seedRemoteHeader(t, ctx, ti.conn, projectID, remote.ID, "Authorization", "Bearer configured-shared-key", "", true)
					// Use the same source-local synchronization as header writes;
					// no organization backfill or permission grants are inserted.
					tx := testenv.BeginTx(t, ctx, ti.conn)
					require.NoError(t, authclientbackfill.SyncRemoteSource(ctx, tx, ac.ActiveOrganizationID, remote.ID))
					require.NoError(t, tx.Commit(ctx))
				}
				if state != "noauth" && state != "shared_static_client" && state != "disabled_typed_client" && state != "legacy_static_client" {
					sourceIssuerID := issuerID
					issuerIDs := []uuid.UUID{issuerID}
					if state == "shared_oauth_registration" {
						siblingIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
						issuerIDs = append(issuerIDs, siblingIssuerID)
						siblingID := mcpServerBySlug(t, ctx, ti, projectID, fx.denied)
						_, err := mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{ID: siblingID, ProjectID: projectID, Name: conv.ToPGText(fx.denied), Slug: conv.ToPGText(fx.denied), RemoteMcpServerID: conv.ToNullUUID(remote.ID), UserSessionIssuerID: conv.ToNullUUID(siblingIssuerID), Visibility: mcpservers.VisibilityPrivate})
						require.NoError(t, err)
					}
					clientID = createConsentRemoteClient(t, ctx, ti.conn, projectID, ac.ActiveOrganizationID, "attachment-client", "", issuerIDs)
					require.NoError(t, upstreamclients.Reconcile(ctx, ti.conn, ac.ActiveOrganizationID, projectID, issuerIDs))
					if state == "sibling_issuer_client" {
						siblingID := mcpServerBySlug(t, ctx, ti, projectID, fx.denied)
						_, err := mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{ID: siblingID, ProjectID: projectID, Name: conv.ToPGText(fx.denied), Slug: conv.ToPGText(fx.denied), RemoteMcpServerID: conv.ToNullUUID(remote.ID), UserSessionIssuerID: conv.ToNullUUID(issuerID), Visibility: mcpservers.VisibilityPrivate})
						require.NoError(t, err)
						siblingClientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, ac.ActiveOrganizationID, "sibling-only-client", "", issuerIDs)
						require.NotEqual(t, clientID, siblingClientID)
						require.NoError(t, testrepo.New(ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{ProjectID: projectID, OrganizationID: ac.ActiveOrganizationID, McpServerID: siblingID, RemoteSessionClientID: siblingClientID}))
						tx := testenv.BeginTx(t, ctx, ti.conn)
						require.NoError(t, authclientbackfill.SyncServerAssociations(ctx, tx, ac.ActiveOrganizationID, siblingID))
						require.NoError(t, tx.Commit(ctx))
						authClients := authclientsrepo.New(ti.conn)
						for _, member := range []struct{ serverID, clientID uuid.UUID }{{serverID, clientID}, {siblingID, siblingClientID}} {
							associated, err := authClients.ListServerAuthClients(ctx, authclientsrepo.ListServerAuthClientsParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: projectID, McpServerID: member.serverID})
							for _, client := range associated {
								require.Equal(t, member.clientID, client.RemoteSessionClientID.UUID)
							}
							require.NoError(t, err)
							require.Len(t, associated, 1)
						}
					}
					owner := urn.NewUserSubject(ac.UserID)
					insertQualifiedRemoteSessionToken(t, ctx, ti, sourceIssuerID, clientID, owner, "exact-owner-connection", upstream.url)
					other := createTestUser(t, ctx, ti, "attachment-decoy-"+uuid.NewString())
					insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, other, "decoy-connection", upstream.url)
					q := remotesessionsrepo.New(ti.conn)
					sessions, err := q.ListPrincipalRemoteSessionCandidates(ctx, remotesessionsrepo.ListPrincipalRemoteSessionCandidatesParams{ProjectID: projectID, OrganizationID: ac.ActiveOrganizationID, UserSessionIssuerID: sourceIssuerID, SubjectUrn: owner})
					require.NoError(t, err)
					require.Len(t, sessions, 1)
					if state != "missing_attachment" {
						binding, err := q.AttachPrincipalRemoteSessionBinding(ctx, remotesessionsrepo.AttachPrincipalRemoteSessionBindingParams{ProjectID: projectID, OrganizationID: ac.ActiveOrganizationID, PrincipalID: fx.agent.ID, UserSessionIssuerID: sourceIssuerID, SubjectUrn: owner.String(), RemoteSessionID: sessions[0].RemoteSession.ID})
						require.NoError(t, err)
						if state == "shared_oauth_registration" {
							// The sibling shares the registration, not attachment authority.
							// Keep the source attachment on the member being called.
							require.Equal(t, issuerID, binding.UserSessionIssuerID)
						}
						if state == "revoked_attachment" {
							_, err = testrepo.New(ti.conn).RevokeAttachmentByIDFixture(ctx, binding.ID)
							require.NoError(t, err)
						}
						if state == "replaced_source_without_reattach" {
							deleted, err := q.SoftDeleteRemoteSessionBySubjectAndClient(ctx, remotesessionsrepo.SoftDeleteRemoteSessionBySubjectAndClientParams{SubjectUrn: owner, RemoteSessionClientID: clientID, UserSessionIssuerID: issuerID, ProjectID: projectID, OrganizationID: ac.ActiveOrganizationID})
							require.NoError(t, err)
							require.Len(t, deleted, 1)
							insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, owner, "replacement-source-token", upstream.url)
							replacements, err := q.ListPrincipalRemoteSessionCandidates(ctx, remotesessionsrepo.ListPrincipalRemoteSessionCandidatesParams{ProjectID: projectID, OrganizationID: ac.ActiveOrganizationID, UserSessionIssuerID: sourceIssuerID, SubjectUrn: owner})
							require.NoError(t, err)
							require.Len(t, replacements, 1)
							replacementID := replacements[0].RemoteSession.ID
							require.NotEqual(t, sessions[0].RemoteSession.ID, replacementID, "a new source requires explicit reattachment")
						}
						if state == "same_source_new_generation" {
							// Same-row reconnect intentionally retains the attachment. Use the
							// callback's normal upsert, not an invented stale-binding rule.
							insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, owner, "reconnected-owner-connection", upstream.url)
							reconnected, err := q.GetRemoteSessionByID(ctx, remotesessionsrepo.GetRemoteSessionByIDParams{ID: sessions[0].RemoteSession.ID, ProjectID: projectID, OrganizationID: ac.ActiveOrganizationID})
							require.NoError(t, err)
							generation := reconnected.GrantGeneration
							require.Equal(t, sessions[0].RemoteSession.GrantGeneration+1, generation)
						}
					}
				}
				require.NoError(t, upstreamclients.Reconcile(ctx, ti.conn, ac.ActiveOrganizationID, projectID, []uuid.UUID{issuerID}))
				fx.token = issueMemberAuthRegressionKey(t, ctx, ti, fx, serverID)
				// Direct lifecycle invalidation fixtures; serving must never repair them.
				switch state {
				case "disabled_typed_client":
					clients, err := authclientsrepo.New(ti.conn).ListServerAuthClients(ctx, authclientsrepo.ListServerAuthClientsParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: projectID, McpServerID: serverID})
					require.NoError(t, err)
					require.Len(t, clients, 1)
					_, err = authclientsrepo.New(ti.conn).UpdateAuthClient(ctx, authclientsrepo.UpdateAuthClientParams{ID: clients[0].ID, OrganizationID: ac.ActiveOrganizationID, Name: "Disabled regression client", Enabled: false})
					require.NoError(t, err)
				case "deleted_client":
					n, err := remotesessionsrepo.New(ti.conn).SoftDeleteRemoteSessionClientFixture(ctx, clientID)
					require.NoError(t, err)
					require.EqualValues(t, 1, n)
				case "disabled_agent":
					n, err := testrepo.New(ti.conn).SuspendAttachmentAgentFixture(ctx, testrepo.SuspendAttachmentAgentFixtureParams{ID: fx.agent.ID, OrganizationID: ac.ActiveOrganizationID})
					require.NoError(t, err)
					require.EqualValues(t, 1, n)
				case "removed_agent_grant":
					q := agentsrepo.New(ti.conn)
					grants, err := q.ListAgentPolicyGrants(ctx, agentsrepo.ListAgentPolicyGrantsParams{OrganizationID: ac.ActiveOrganizationID, AgentID: fx.agent.ID})
					require.NoError(t, err)
					removed := 0
					for _, grant := range grants {
						if grant.Scope != string(authz.ScopeMCPConnect) {
							continue
						}
						deleted, err := q.DeleteAgentPolicyGrant(ctx, agentsrepo.DeleteAgentPolicyGrantParams{OrganizationID: ac.ActiveOrganizationID, AgentID: fx.agent.ID, GrantID: grant.ID})
						require.NoError(t, err)
						require.Equal(t, grant.ID, deleted.ID)
						removed++
					}
					require.Equal(t, 1, removed)
				case "credential_block":
					// Credentials this agent can't use: mcp:blocked_auth on the
					// credential itself, written through the policy writer.
					clients, err := authclientsrepo.New(ti.conn).ListServerAuthClients(ctx, authclientsrepo.ListServerAuthClientsParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: projectID, McpServerID: serverID})
					require.NoError(t, err)
					require.Len(t, clients, 1)
					projectIDText := projectID.String()
					agents := agentmanagement.NewService(ti.logger, ti.tracerProvider, ti.conn, ti.sessionManager, ti.authzEngine, ti.audit, nil, nil)
					_, err = agents.CreatePolicyGrant(ctx, &agentsgen.CreatePolicyGrantPayload{AgentID: fx.agent.ID.String(), Scope: string(authz.ScopeMCPBlockedAuth), Effect: "allow", Selector: &agentsgen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCPAuthClient, ResourceID: clients[0].ID.String(), ProjectID: &projectIDText}})
					require.NoError(t, err)
				case "owner_block":
					// Connection-owner blocks are mcp:blocked_auth on the agent,
					// not an effect=deny mutation to the owner's mcp:connect grant.
					// Prove admission in the selected mode BEFORE writing the block. A missing
					// normal auth-client/grant writer must fail here, not masquerade
					// as successful block enforcement. Never repair that failure.
					baseline, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute_tool", "arguments": map[string]any{"name": fx.granted + "--ping", "arguments": map[string]any{}}}))
					require.NoError(t, err)
					baselineEnvelope := decodeRPCResponse(t, baseline)
					require.NotContains(t, baselineEnvelope, "error", "pre-block baseline must succeed: %s", baseline.Body.String())
					text, isError := metaToolResultText(t, baselineEnvelope)
					require.False(t, isError, "pre-block baseline: %s", text)
					var baselineCredentials []string
					for _, request := range upstream.journal() {
						if request.rpcMethod == "tools/call" {
							baselineCredentials = append(baselineCredentials, request.authorization)
						}
					}
					require.Equal(t, []string{"Bearer exact-owner-connection"}, baselineCredentials, "pre-block baseline")
					upstream.mu.Lock()
					upstream.requests = nil
					upstream.mu.Unlock()
					attempts.Store(0)
					rejected.Store(0)
					// Restrictive stored-state fixture uses the connection-block writer,
					// as in the original regression. Do not fabricate a rollout flag
					// to exercise the separately gated management endpoint here.
					selector, err := authz.ConnectionAuthClientSelector(clientID.String(), ac.UserID).MarshalJSON()
					require.NoError(t, err)
					inserted, err := agentsrepo.New(ti.conn).CreateConnectionBlockGrant(ctx, agentsrepo.CreateConnectionBlockGrantParams{OrganizationID: ac.ActiveOrganizationID, AgentID: fx.agent.ID, Selectors: selector})
					require.NoError(t, err)
					require.True(t, inserted)
				}
				snapshot := func() string {
					value, err := fixturerepo.New(ti.conn).SnapshotAgentMemberAuthority(ctx, fx.agent.ID)
					require.NoError(t, err)
					return value
				}
				before := snapshot()
				body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute_tool", "arguments": map[string]any{"name": fx.granted + "--ping", "arguments": map[string]any{}}})
				response, callErr := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, body)
				var credentials []string
				for _, request := range upstream.journal() {
					require.NotEqual(t, "Bearer "+fx.token, request.authorization)
					require.NotEqual(t, "Bearer decoy-connection", request.authorization)
					if request.rpcMethod == "tools/call" {
						credentials = append(credentials, request.authorization)
					}
				}
				// Connection-owner policy remains connect-equivalent: only enforced
				// mode applies typed-client enablement and mcp:blocked_auth.
				allowed := state == "sibling_issuer_client" || state == "live" || state == "same_source_new_generation" || state == "noauth" || state == "shared_static_client" || state == "shared_oauth_registration" || ((state == "owner_block" || state == "credential_block" || state == "disabled_typed_client" || state == "legacy_static_client") && mode != "enforced") ||
					// Under enforcement an agent key without a usable attachment is
					// served by its authorizer's own connection when the agent's own
					// mcp:auth permits that owner; before enforcement it is denied.
					((state == "missing_attachment" || state == "revoked_attachment" || state == "replaced_source_without_reattach") && mode == "enforced")
				if allowed {
					require.NoError(t, callErr)
					require.Equal(t, http.StatusOK, response.Code)
					text, isError := metaToolResultText(t, decodeRPCResponse(t, response))
					require.False(t, isError, text)
					expected := "Bearer exact-owner-connection"
					if state == "same_source_new_generation" {
						expected = "Bearer reconnected-owner-connection"
					}
					if state == "replaced_source_without_reattach" {
						expected = "Bearer replacement-source-token"
					}
					if state == "noauth" || state == "legacy_static_client" {
						expected = ""
					}
					if state == "shared_static_client" || state == "disabled_typed_client" {
						expected = "Bearer configured-shared-key"
					}
					require.Equal(t, []string{expected}, credentials)
					require.Zero(t, rejected.Load(), "successful control must authenticate upstream")
				} else {
					require.Zero(t, attempts.Load(), "local denial must not be mistaken for an upstream rejection")
					require.Empty(t, credentials, "denied attachment must execute zero tools")
					upstream.requireNeverCarried(t, "Bearer exact-owner-connection")
					upstream.requireNeverCarried(t, "Bearer replacement-source-token")
					if callErr == nil {
						envelope := decodeRPCResponse(t, response)
						if rpcError, ok := envelope["error"]; ok {
							require.NotEqual(t, "null", string(rpcError), "a denied gateway member may return a JSON-RPC error")
							require.NotContains(t, envelope, "result")
						} else {
							text, isError := metaToolResultText(t, envelope)
							require.True(t, isError, text)
						}
					}
				}
				// Before enforcement the direct endpoint keeps the issuer-wide
				// resolution, which a sibling server's client still constrains.
				if state == "live" || state == "owner_block" || state == "credential_block" || (state == "sibling_issuer_client" && mode == "enforced") {
					// The same key on the server's own endpoint must reach the
					// gateway's decision: a block the gateway enforces cannot be
					// bypassed by calling the server directly.
					upstream.mu.Lock()
					upstream.requests = nil
					upstream.mu.Unlock()
					attempts.Store(0)
					rejected.Store(0)
					_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
						ProjectID:       projectID,
						CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
						McpServerID:     conv.ToNullUUID(serverID),
						MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
						Slug:            fx.granted,
					})
					require.NoError(t, err)
					direct := serveAgentKeyDirect(t, ti, fx.granted, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "ping", "arguments": map[string]any{}}), fx.token)
					var directCredentials []string
					for _, request := range upstream.journal() {
						require.NotEqual(t, "Bearer decoy-connection", request.authorization)
						if request.rpcMethod == "tools/call" {
							directCredentials = append(directCredentials, request.authorization)
						}
					}
					if allowed {
						require.Equal(t, http.StatusOK, direct.Code, "direct: %s", direct.Body.String())
						require.Equal(t, []string{"Bearer exact-owner-connection"}, directCredentials, "direct")
					} else {
						require.Zero(t, attempts.Load(), "direct: a blocked agent key must not reach upstream: %d %s", direct.Code, direct.Body.String())
						require.Empty(t, directCredentials, "direct")
						require.Equal(t, http.StatusForbidden, direct.Code, "direct: %s", direct.Body.String())
					}
				}
				require.Equal(t, before, snapshot(), "serving must not repair attachment authority")
			})
		}
	}
}

// serveAgentKeyDirect calls a server's own endpoint with an agent key, as an
// MCP client configured with the key would.
func serveAgentKeyDirect(t *testing.T, ti *testInstance, slug string, body []byte, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug, bytes.NewReader(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	oops.ErrHandle(ti.logger, ti.service.ServePublic).ServeHTTP(w, req)
	return w
}

// issueMemberAuthRegressionKey uses the same grant and issuance boundaries as management.
func issueMemberAuthRegressionKey(t *testing.T, ctx context.Context, ti *testInstance, fx agentGatewayFixture, serverID uuid.UUID) string {
	t.Helper()
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := ac.ProjectID.String()
	agentID := fx.agent.ID.String()
	audience := authclients.NewService(ti.logger, ti.tracerProvider, ti.conn, ti.sessionManager, ti.authzEngine, ti.audit, ti.enc)
	agents := agentmanagement.NewService(ti.logger, ti.tracerProvider, ti.conn, ti.sessionManager, ti.authzEngine, ti.audit, nil, nil)
	requested := []*keysgen.AgentPolicyGrantForm{{Scope: string(authz.ScopeMCPConnect), Effect: "allow", Selector: &keysgen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: serverID.String(), ProjectID: &projectID}}}
	clients, err := authclientsrepo.New(ti.conn).ListServerAuthClients(ctx, authclientsrepo.ListServerAuthClientsParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, McpServerID: serverID})
	require.NoError(t, err)
	for _, client := range clients {
		id := client.ID
		current, err := audience.GetAuthClientAudience(ctx, &authclientsgen.GetAuthClientAudiencePayload{ID: id.String()})
		require.NoError(t, err)
		_, err = audience.SetAuthClientAudience(ctx, &authclientsgen.SetAuthClientAudiencePayload{ID: id.String(), ExpectedVersion: current.Version, Entries: []*authclientsgen.AuthClientAudienceRule{{PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID).String(), Level: authclients.LevelUse}}})
		require.NoError(t, err)
		clientType := client.ClientType
		var owner *string
		if clientType == authz.AuthClientTypeRemoteSession {
			owner = new(authz.SelectorValueRepresentedUser)
		}
		_, err = agents.CreatePolicyGrant(ctx, &agentsgen.CreatePolicyGrantPayload{AgentID: agentID, Scope: string(authz.ScopeMCPAuth), Effect: "allow", Selector: &agentsgen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCPAuthClient, ResourceID: id.String(), ProjectID: &projectID, ClientType: &clientType, ConnectionOwner: owner}})
		require.NoError(t, err)
	}
	// General key issuance is v4. Its existing project-bounded auth ceiling
	// still intersects the explicit live owner and agent auth grants above.
	// Do not invent a v5 key or broaden the production issuance contract.
	version := int(runtimepolicy.IssuedDelegatedPolicyVersion)
	issued, err := keys.NewService(ti.logger, ti.tracerProvider, ti.conn, ti.sessionManager, "local", ti.authzEngine, ti.audit).CreateKey(ctx, &keysgen.CreateKeyPayload{Name: "Authorized member regression key", AgentID: &agentID, DelegatedGrantsVersion: &version, RequestedGrants: requested})
	require.NoError(t, err)
	require.NotNil(t, issued.Key)
	return *issued.Key
}
