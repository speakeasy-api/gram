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

	"github.com/speakeasy-api/gram/server/internal/auth"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	require.NoError(t, testrepo.New(ti.conn).ExpireAPIKeyFixture(ctx, parent.ID))

	_, err = serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), child, makeInitializeBody())
	requireAgentGatewayCode(t, err, oops.CodeUnauthorized)
}

func TestApplyIssuerGate_MCPCredentialLivesAndDiesWithParent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx, _, _, session := seedAgentRefreshSession(t, ctx, ti)
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
	child := insertMCPCredential(t, ctx, ti, parentToken)
	endpoint := mcp.ResolvedMcpEndpoint{
		AudienceURN: urn.NewToolset(fx.toolset.ID).String(), OrganizationID: fx.orgID,
		ProjectID: fx.target.ProjectID, RouteBase: "mcp", Slug: fx.toolset.McpSlug.String,
		ToolsetID: uuid.NullUUID{UUID: fx.toolset.ID, Valid: true}, UserSessionIssuerID: fx.target.UserSessionIssuerID,
	}

	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), child, ti.serverURL.String(), &endpoint)
	require.NoError(t, err)

	revokeKeyByToken(t, ctx, ti, parentToken)
	w := httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), w, child, ti.serverURL.String(), &endpoint)
	assertAgentKeyUnauthorized(t, w, err)
}
