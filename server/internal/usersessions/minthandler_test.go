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
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestMintUserSessionRequiresMCPConnect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	toolset := createIssuerGatedMintToolset(t, ctx, ti, "mint-denied")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// project:read used to be enough to mint. The mint bearer grants runtime
	// access, so the endpoint must require the same mcp:connect permission the
	// runtime gate enforces.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)

	toolsetID := toolset.ID.String()
	_, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
		ToolsetID:        &toolsetID,
		McpServerID:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, mcpaccess.ServerPermissionDeniedMessage, oopsErr.Error())
}

func TestMintUserSessionAllowsMCPConnect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	toolset := createIssuerGatedMintToolset(t, ctx, ti, "mint-allowed")

	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPConnect, toolset.ID.String()),
	)

	toolsetID := toolset.ID.String()
	got, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
		ToolsetID:        &toolsetID,
		McpServerID:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, got.AccessToken)
	require.Equal(t, 3600, got.ExpiresIn)

	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(
		got.AccessToken,
		urn.NewToolset(toolset.ID).String(),
	)
	require.NoError(t, err)

	row, err := repo.New(ti.conn).GetUserSessionByJTI(ctx, repo.GetUserSessionByJTIParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Jti:                 claims.ID,
	})
	require.NoError(t, err)
	require.False(t, row.UserSessionClientID.Valid)
	require.True(t, strings.HasPrefix(row.RefreshTokenHash, "dashboard-mint:"))
}

func TestMintUserSessionRequiresExactlyOneTarget(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Neither target set → bad request.
	_, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
		ToolsetID:        nil,
		McpServerID:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Both targets set → bad request (mutually exclusive).
	toolsetID := uuid.New().String()
	serverID := uuid.New().String()
	_, err = ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
		ToolsetID:        &toolsetID,
		McpServerID:      &serverID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func createIssuerGatedMintToolset(t *testing.T, ctx context.Context, ti *testInstance, slug string) toolsetsrepo.Toolset {
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

	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: slug, Valid: true},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	linked, err := toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.MustParse(issuer.ID), Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           toolset.ProjectID,
	})
	require.NoError(t, err)

	return linked
}
