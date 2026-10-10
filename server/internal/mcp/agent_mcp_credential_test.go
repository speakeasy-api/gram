package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// insertMCPCredential stores a child of the key behind parentToken the way
// agent.mintMcpCredential does, carrying the parent's subject and policy.
func insertMCPCredential(t *testing.T, ctx context.Context, ti *testInstance, parentToken string) string {
	t.Helper()
	parentHash, err := auth.GetAPIKeyHash(parentToken)
	require.NoError(t, err)
	parent, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, parentHash)
	require.NoError(t, err)

	token, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateChildAgentAPIKey(ctx, keysrepo.CreateChildAgentAPIKeyParams{
		OrganizationID: parent.OrganizationID, CreatedByUserID: parent.CreatedByUserID, Name: "MCP credential " + parent.ID.String(),
		KeyPrefix: prefix, KeyHash: hash, SubjectUrn: parent.SubjectUrn,
		DelegatedGrants: parent.DelegatedGrants, DelegatedGrantsVersion: parent.DelegatedGrantsVersion,
		ExpiresAt: parent.ExpiresAt, ParentApiKeyID: uuid.NullUUID{UUID: parent.ID, Valid: true},
	})
	require.NoError(t, err)
	return token
}

func revokeKeyByToken(t *testing.T, ctx context.Context, ti *testInstance, token string) {
	t.Helper()
	hash, err := auth.GetAPIKeyHash(token)
	require.NoError(t, err)
	key, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, hash)
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).DeleteAgentAPIKey(ctx, keysrepo.DeleteAgentAPIKeyParams{ID: key.ID, OrganizationID: key.OrganizationID, SubjectUrn: key.SubjectUrn})
	require.NoError(t, err)
}

func TestServeAgentGateway_MCPCredentialLivesAndDiesWithParent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)
	child := insertMCPCredential(t, ctx, ti, fx.token)

	w, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), child, makeInitializeBody())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	revokeKeyByToken(t, ctx, ti, fx.token)
	_, err = serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), child, makeInitializeBody())
	requireAgentGatewayCode(t, err, oops.CodeUnauthorized)
}

func TestServeAgentGateway_MCPCredentialDiesWithExpiredParent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)
	child := insertMCPCredential(t, ctx, ti, fx.token)

	hash, err := auth.GetAPIKeyHash(fx.token)
	require.NoError(t, err)
	parent, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, hash)
	require.NoError(t, err)
	require.NoError(t, testrepo.New(ti.conn).ExpireAPIKeyFixture(ctx, testrepo.ExpireAPIKeyFixtureParams{ID: parent.ID, OrganizationID: parent.OrganizationID}))

	_, err = serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), child, makeInitializeBody())
	requireAgentGatewayCode(t, err, oops.CodeUnauthorized)
}

// Suspension lives on the agent, not the key, so it reaches a child through
// live principal admission rather than the parent-liveness predicate.
func TestServeAgentGateway_MCPCredentialDiesWithSuspendedAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)
	child := insertMCPCredential(t, ctx, ti, fx.token)

	_, err := agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: fx.agent.OrganizationID, ID: fx.agent.ID})
	require.NoError(t, err)
	_, err = serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), child, makeInitializeBody())
	requireAgentGatewayCode(t, err, oops.CodeUnauthorized)
}

func TestApplyIssuerGate_MCPCredentialDiesWithSuspendedAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx, agent, _, session := seedAgentRefreshSession(t, ctx, ti)
	_, child, endpoint := issuerGateMCPCredential(t, ctx, ti, fx, session)

	_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), child, ti.serverURL.String(), &endpoint)
	require.NoError(t, err)

	_, err = agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), w, child, ti.serverURL.String(), &endpoint)
	assertAgentKeyUnauthorized(t, w, err)
}

// issuerGateMCPCredential enrolls a key for the session's agent and mints a
// child of it, returning both tokens and the issuer-gated endpoint.
func issuerGateMCPCredential(t *testing.T, ctx context.Context, ti *testInstance, fx agentConsentFixture, session usersessions_repo.UserSession) (string, string, mcp.ResolvedMcpEndpoint) {
	t.Helper()
	parentToken, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{
		OrganizationID: fx.orgID, CreatedByUserID: fx.userID, Name: "issuer gate enrollment",
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
	return parentToken, insertMCPCredential(t, ctx, ti, parentToken), endpoint
}

func TestApplyIssuerGate_MCPCredentialLivesAndDiesWithParent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx, _, _, session := seedAgentRefreshSession(t, ctx, ti)
	parentToken, child, endpoint := issuerGateMCPCredential(t, ctx, ti, fx, session)

	_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), child, ti.serverURL.String(), &endpoint)
	require.NoError(t, err)

	revokeKeyByToken(t, ctx, ti, parentToken)
	w := httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), w, child, ti.serverURL.String(), &endpoint)
	assertAgentKeyUnauthorized(t, w, err)
}
