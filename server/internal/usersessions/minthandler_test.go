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
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
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

	// No registered OAuth client backs this mint, so the claim names our own
	// surface rather than going out empty — empty would be indistinguishable
	// from a token minted before the claim existed.
	require.Equal(t, usersessions.FirstPartyClientID, claims.ClientID)
	require.NotContains(t, claims.ClientID, "://",
		"a URL-shaped client id would be read as an OAuth Client ID Metadata Document")
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

func TestMintUserSessionForMetaMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	meta, issuerID := createIssuerGatedMintMetaServer(t, ctx, ti, "mint-gateway")

	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPConnect, meta.ID.String()),
	)

	metaID := meta.ID.String()
	got, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
		ToolsetID:        nil,
		McpServerID:      nil,
		MetaMcpServerID:  &metaID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, got.AccessToken)

	// The audience is the issuer URN, which is what
	// NewResolvedMcpEndpointFromMetaMcpServer validates the bearer against.
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(
		got.AccessToken,
		urn.NewUserSessionIssuer(issuerID).String(),
	)
	require.NoError(t, err)
	require.Contains(t, claims.Issuer, "/mcp/mint-gateway-endpoint")
}

func TestMintUserSessionForMetaMcpServerRequiresMCPConnect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	meta, _ := createIssuerGatedMintMetaServer(t, ctx, ti, "mint-gateway-denied")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// A grant restricted to some other resource must not mint for the gateway:
	// the bearer would otherwise hand its holder runtime access.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPConnect, uuid.New().String()),
	)

	metaID := meta.ID.String()
	_, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
		ToolsetID:        nil,
		McpServerID:      nil,
		MetaMcpServerID:  &metaID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestMintUserSessionForMetaMcpServerWithoutAddress(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "mint-gateway-noaddr-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerID := uuid.MustParse(issuer.ID)

	// No mcp_endpoints row: the issuer claim has no address to name, so the
	// mint refuses rather than emitting a token pointing nowhere.
	meta, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Name:                "mint-gateway-noaddr",
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
	})
	require.NoError(t, err)

	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPConnect, meta.ID.String()),
	)

	metaID := meta.ID.String()
	_, err = ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
		ToolsetID:        nil,
		McpServerID:      nil,
		MetaMcpServerID:  &metaID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func createIssuerGatedMintMetaServer(t *testing.T, ctx context.Context, ti *testInstance, slug string) (metamcprepo.MetaMcpServer, uuid.UUID) {
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
	issuerID := uuid.MustParse(issuer.ID)

	meta, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Name:                slug,
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
	})
	require.NoError(t, err)

	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		MetaMcpServerID: uuid.NullUUID{UUID: meta.ID, Valid: true},
		Slug:            slug + "-endpoint",
	})
	require.NoError(t, err)

	return meta, issuerID
}
