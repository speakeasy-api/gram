package usersessions_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	sessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestMintUserSessionForServerRequiresMCPConnect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createIssuerGatedMintServer(t, ctx, ti, "mint-server-denied")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// project:read is not enough — minting a bearer grants runtime access, so
	// the endpoint must require the same mcp:connect permission the runtime
	// gate enforces.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)

	_, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
		ToolsetID:        nil,
		McpServerID:      conv.PtrEmpty(server.ID.String()),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, mcpaccess.ServerPermissionDeniedMessage, oopsErr.Error())
}

func TestMintUserSessionForServerAllowsMCPConnect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createIssuerGatedMintServer(t, ctx, ti, "mint-server-allowed")

	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPConnect, server.ID.String()),
	)

	got, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
		ToolsetID:        nil,
		McpServerID:      conv.PtrEmpty(server.ID.String()),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, got.AccessToken)
	require.Equal(t, 3600, got.ExpiresIn)

	// Remote-server tokens are bound to the user_session_issuer audience (the
	// /x/mcp convention), not a toolset.
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(
		got.AccessToken,
		urn.NewUserSessionIssuer(server.UserSessionIssuerID.UUID).String(),
	)
	require.NoError(t, err)

	row, err := repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{
		UserSessionIssuerID: server.UserSessionIssuerID.UUID,
		Jti:                 claims.ID,
	})
	require.NoError(t, err)
	require.False(t, row.UserSessionClientID.Valid)
	require.True(t, strings.HasPrefix(row.RefreshTokenHash, "dashboard-mint:"))
}

func TestMintUserSessionForServerRejectsUngatedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	// A server with no user_session_issuer_id can't be minted against.
	toolset := createBackingToolset(t, ctx, ti, "mint-server-ungated")
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             *authCtx.ProjectID,
		Name:                  pgtype.Text{String: "mint-server-ungated", Valid: true},
		Slug:                  pgtype.Text{String: "mint-server-ungated", Valid: true},
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPConnect, server.ID.String()),
	)

	_, err = ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
		ToolsetID:        nil,
		McpServerID:      conv.PtrEmpty(server.ID.String()),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// createIssuerGatedMintServer creates an issuer-gated mcp_server. It's backed by
// a toolset (the backend-exclusivity constraint requires exactly one of
// toolset_id / remote_mcp_server_id) so the fixture stays lightweight — the
// mint handler only reads user_session_issuer_id and slug, which a remote-backed
// server populates identically.
func createIssuerGatedMintServer(t *testing.T, ctx context.Context, ti *testInstance, slug string) mcpserversrepo.McpServer {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 slug + "-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	toolset := createBackingToolset(t, ctx, ti, slug)

	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             *authCtx.ProjectID,
		Name:                  pgtype.Text{String: slug, Valid: true},
		Slug:                  pgtype.Text{String: slug, Valid: true},
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.MustParse(issuer.ID), Valid: true},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

	return server
}

func createBackingToolset(t *testing.T, ctx context.Context, ti *testInstance, slug string) toolsetsrepo.Toolset {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   slug + "-backing",
		Slug:                   slug + "-backing",
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	return toolset
}
