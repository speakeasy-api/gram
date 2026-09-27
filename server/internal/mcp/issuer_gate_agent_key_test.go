package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestApplyIssuerGate_AgentAPIKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx, agent, _, session := seedAgentRefreshSession(t, ctx, ti)
	token, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	key, err := keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{
		OrganizationID: fx.orgID, CreatedByUserID: fx.userID, Name: "issuer gate agent",
		KeyPrefix: prefix, KeyHash: hash,
		SubjectUrn:      pgtype.Text{String: session.SubjectUrn.String(), Valid: true},
		DelegatedGrants: session.DelegatedGrants, DelegatedGrantsVersion: session.DelegatedGrantsVersion,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	require.NoError(t, err)
	endpoint := mcp.ResolvedMcpEndpoint{
		AudienceURN: urn.NewToolset(fx.toolset.ID).String(), OrganizationID: fx.orgID,
		ProjectID: fx.target.ProjectID, RouteBase: "mcp", Slug: fx.toolset.McpSlug.String,
		ToolsetID: uuid.NullUUID{UUID: fx.toolset.ID, Valid: true}, UserSessionIssuerID: fx.target.UserSessionIssuerID,
	}
	for _, flag := range []feature.Flag{feature.FlagAgentManagement, feature.FlagAgentIdentityCredentials} {
		ti.features.SetFlag(flag, fx.orgID, false)
		_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), &endpoint)
		var denied *oops.ShareableError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, oops.CodeNotFound, denied.Code)
		ti.features.SetFlag(flag, fx.orgID, true)
	}
	w := httptest.NewRecorder()
	admitted, _, selection, err := ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), &endpoint)
	require.NoError(t, err)
	require.Nil(t, selection)
	actor, ok := contextvalues.AuthenticatedActor(admitted)
	require.True(t, ok)
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), actor)
	identity, ok := mcpidentity.FromContext(admitted)
	require.True(t, ok)
	require.Equal(t, mcpidentity.KindAgent, identity.Kind())
	require.Equal(t, agent.ID.String(), identity.AgentID())
	require.Empty(t, identity.UserID())
	authCtx, ok := contextvalues.GetAuthContext(admitted)
	require.True(t, ok)
	require.Equal(t, key.ID.String(), authCtx.APIKeyID)
	require.Equal(t, endpoint.ProjectID, *authCtx.ProjectID)
	credential, ok := contextvalues.PrincipalCredentialAuthorization(admitted)
	require.True(t, ok)
	require.Equal(t, session.DelegatedGrants, credential.DelegatedGrants)

	for _, mismatch := range []string{"organization", "project", "resource"} {
		func() {
			t.Log(mismatch)
			other := endpoint
			switch mismatch {
			case "organization":
				other.OrganizationID = "org-other"
			case "project":
				other.ProjectID = uuid.New()
			case "resource":
				other.ToolsetID.UUID = uuid.New()
			}
			w := httptest.NewRecorder()
			_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), &other)
			require.Error(t, err)
			assertAgentKeyUnauthorized(t, w, err)
		}()
	}

	legacyToken, legacyHash, legacyPrefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID: fx.orgID, CreatedByUserID: fx.userID, Name: "legacy issuer gate rejection",
		KeyHash: legacyHash, KeyPrefix: legacyPrefix, Scopes: []string{"consumer"},
	})
	require.NoError(t, err)
	w = httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), w, legacyToken, ti.serverURL.String(), &endpoint)
	require.Error(t, err)
	assertAgentKeyUnauthorized(t, w, err)
	for _, flag := range []feature.Flag{feature.FlagAgentManagement, feature.FlagAgentIdentityCredentials} {
		ti.features.SetFlag(flag, fx.orgID, false)
		w = httptest.NewRecorder()
		_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), w, legacyToken, ti.serverURL.String(), &endpoint)
		assertAgentKeyUnauthorized(t, w, err)
		ti.features.SetFlag(flag, fx.orgID, true)
	}

	// Requiring an upstream must fail closed without an attachment, even
	// when an old direct-subject remote session exists for this agent.
	remoteRepo := remotesessionsrepo.New(ti.conn)
	remoteIssuer, err := remoteRepo.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID: uuid.NullUUID{UUID: endpoint.ProjectID, Valid: true},
		Slug:      "agent-key-upstream", Issuer: "https://upstream.example",
		ScopesSupported: []string{}, GrantTypesSupported: []string{"authorization_code"},
		ResponseTypesSupported: []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	remoteClient, err := remoteRepo.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             uuid.NullUUID{UUID: endpoint.ProjectID, Valid: true},
		RemoteSessionIssuerID: remoteIssuer.ID, ClientID: "agent-key-client",
	})
	require.NoError(t, err)
	require.NoError(t, remoteRepo.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: remoteClient.ID, UserSessionIssuerID: endpoint.UserSessionIssuerID,
	}))
	insertRemoteSessionAccessToken(t, ctx, ti, endpoint.UserSessionIssuerID, remoteClient.ID, session.SubjectUrn, "must-not-use-direct-agent-token", time.Now().Add(time.Hour))
	w = httptest.NewRecorder()
	_, tokens, _, err := ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), &endpoint)
	require.Error(t, err, "missing attachment must stop dispatch, not use direct sessions or environment fallback")
	require.Nil(t, tokens)
	assertAgentKeyUnauthorized(t, w, err)

	ownerSession := insertRemoteSessionAccessToken(t, ctx, ti, endpoint.UserSessionIssuerID, remoteClient.ID, urn.NewUserSubject(fx.userID), "attached-owner-token", time.Now().Add(time.Hour))
	attach := func() remotesessionsrepo.AttachPrincipalRemoteSessionBindingRow {
		t.Helper()
		binding, err := remoteRepo.AttachPrincipalRemoteSessionBinding(ctx, remotesessionsrepo.AttachPrincipalRemoteSessionBindingParams{
			ProjectID: endpoint.ProjectID, OrganizationID: fx.orgID, PrincipalID: agent.ID,
			UserSessionIssuerID: endpoint.UserSessionIssuerID, SubjectUrn: ownerSession.SubjectUrn.String(), RemoteSessionID: ownerSession.ID,
		})
		require.NoError(t, err)
		return binding
	}
	binding := attach()
	agentBearer, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject: session.SubjectUrn, Audience: endpoint.AudienceURN,
		Issuer:    ti.serverURL.JoinPath("mcp", endpoint.Slug).String(),
		ExpiresAt: &session.ExpiresAt.Time, ClientID: fx.client.ClientID, JTI: session.Jti,
	})
	require.NoError(t, err)
	for _, bearer := range []string{token, agentBearer} {
		w = httptest.NewRecorder()
		callCtx, resolved, _, err := ti.service.ApplyIssuerGate(t.Context(), w, bearer, ti.serverURL.String(), &endpoint)
		require.NoError(t, err)
		require.Equal(t, "attached-owner-token", resolved[remoteIssuer.ID].Token)
		actor, ok := contextvalues.AuthenticatedActor(callCtx)
		require.True(t, ok)
		require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), actor)
	}
	_, err = testrepo.New(ti.conn).RevokeAttachmentByIDFixture(ctx, binding.ID)
	require.NoError(t, err)
	for _, bearer := range []string{token, agentBearer} {
		w = httptest.NewRecorder()
		_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, bearer, ti.serverURL.String(), &endpoint)
		assertAgentKeyUnauthorized(t, w, err)
	}
	attach()
	_, err = keysrepo.New(ti.conn).DeleteAgentAPIKey(ctx, keysrepo.DeleteAgentAPIKeyParams{
		ID: key.ID, OrganizationID: fx.orgID, SubjectUrn: pgtype.Text{String: session.SubjectUrn.String(), Valid: true},
	})
	require.NoError(t, err)
	w = httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(admitted, w, token, ti.serverURL.String(), &endpoint)
	assertAgentKeyUnauthorized(t, w, err)
	w = httptest.NewRecorder()
	_, resolved, _, err := ti.service.ApplyIssuerGate(t.Context(), w, agentBearer, ti.serverURL.String(), &endpoint)
	require.NoError(t, err, "revoking the API key must preserve the shared upstream session and other credentials")
	require.Equal(t, "attached-owner-token", resolved[remoteIssuer.ID].Token)

	_, err = agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)
	w = httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(admitted, w, agentBearer, ti.serverURL.String(), &endpoint)
	require.Error(t, err, "previously admitted contexts cannot bypass live admission")
	assertAgentKeyUnauthorized(t, w, err)
}

// ApplyIssuerGate sets the challenge and returns an error; the route wrapper
// renders its HTTP status. Exercise both instead of reading an unwritten recorder.
func assertAgentKeyUnauthorized(t *testing.T, w *httptest.ResponseRecorder, err error) {
	t.Helper()
	require.Error(t, err)
	oops.MCPErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error { return err }).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.NotEmpty(t, w.Header().Get("WWW-Authenticate"))
}
