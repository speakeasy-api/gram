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
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
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

// TestMintUserSessionForServerUsesPrimaryEndpointIssuer pins the iss claim
// to the server's primary endpoint URL when the server has mcp_endpoints
// rows; the legacy /x/mcp/{slug} shape only applies to servers with no
// endpoints (covered by TestMintUserSessionForServerAllowsMCPConnect, whose
// fixture creates none).
func TestMintUserSessionForServerUsesPrimaryEndpointIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createIssuerGatedMintServer(t, ctx, ti, "mint-server-endpoint")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	endpointSlug := "mint-ep-" + uuid.NewString()[:8]
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: server.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            endpointSlug,
	})
	require.NoError(t, err)

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

	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(
		got.AccessToken,
		urn.NewUserSessionIssuer(server.UserSessionIssuerID.UUID).String(),
	)
	require.NoError(t, err)
	require.Equal(t, "http://0.0.0.0/mcp/"+endpointSlug, claims.Issuer,
		"iss must derive from the server's primary endpoint")
}

// TestMintUserSessionForToolsetResolvesWrapper pins the AIS-634 contract for
// the toolset addressing arm: when the toolset has an mcp_servers wrapper the
// mint is governed by the wrapper — issuer-URN audience, RBAC against the
// wrapper id, endpoint-derived iss — even though the toolset row itself
// carries no issuer.
func TestMintUserSessionForToolsetResolvesWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createIssuerGatedMintServer(t, ctx, ti, "mint-toolset-wrapper")
	require.True(t, server.ToolsetID.Valid)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	endpointSlug := "mint-wrapper-ep-" + uuid.NewString()[:8]
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: server.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            endpointSlug,
	})
	require.NoError(t, err)

	// The RBAC resource for a wrapped toolset is the wrapper id, matching the
	// runtime gate after the migration's grant rewrite.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPConnect, server.ID.String()),
	)

	got, err := ti.service.MintUserSession(ctx, &sessionsgen.MintUserSessionPayload{
		ToolsetID:        conv.PtrEmpty(server.ToolsetID.UUID.String()),
		McpServerID:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(
		got.AccessToken,
		urn.NewUserSessionIssuer(server.UserSessionIssuerID.UUID).String(),
	)
	require.NoError(t, err)
	require.Equal(t, "http://0.0.0.0/mcp/"+endpointSlug, claims.Issuer)
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

// TestMintUserSessionForToolsetIgnoresDisabledWrapper pins that a disabled
// wrapper does not govern the mint: the runtime refuses disabled wrappers and
// serves the legacy route, so the mint must produce the legacy toolset binding.
func TestMintUserSessionForToolsetIgnoresDisabledWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	toolset := createIssuerGatedMintToolset(t, ctx, ti, "mint-disabled-wrapper")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	wrapperIssuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "mint-disabled-wrapper-srv-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             *authCtx.ProjectID,
		Name:                  pgtype.Text{String: "mint-disabled-wrapper-srv", Valid: true},
		Slug:                  pgtype.Text{String: "mint-disabled-wrapper-srv", Valid: true},
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.MustParse(wrapperIssuer.ID), Valid: true},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            mcpservers.VisibilityDisabled,
	})
	require.NoError(t, err)

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

	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(
		got.AccessToken,
		urn.NewToolset(toolset.ID).String(),
	)
	require.NoError(t, err)
	require.Equal(t, "http://0.0.0.0/mcp/"+toolset.McpSlug.String, claims.Issuer)
}

// TestMintUserSessionForToolsetIgnoresIssuerlessWrapper pins that a wrapper
// without a user session issuer does not govern the mint; the toolset's own
// issuer binding still applies. Production has manually created wrappers in
// exactly this shape.
func TestMintUserSessionForToolsetIgnoresIssuerlessWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	toolset := createIssuerGatedMintToolset(t, ctx, ti, "mint-issuerless-wrapper")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	_, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             *authCtx.ProjectID,
		Name:                  pgtype.Text{String: "mint-issuerless-wrapper-srv", Valid: true},
		Slug:                  pgtype.Text{String: "mint-issuerless-wrapper-srv", Valid: true},
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

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

	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(
		got.AccessToken,
		urn.NewToolset(toolset.ID).String(),
	)
	require.NoError(t, err)
	require.Equal(t, "http://0.0.0.0/mcp/"+toolset.McpSlug.String, claims.Issuer)
}

// TestMintUserSessionForToolsetIgnoresEndpointlessWrapper pins that an
// issuer-gated wrapper with no endpoint does not govern the mint: nothing serves
// it, so a wrapper-bound token would be rejected everywhere.
func TestMintUserSessionForToolsetIgnoresEndpointlessWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	toolset := createIssuerGatedMintToolset(t, ctx, ti, "mint-endpointless-wrapper")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	wrapperIssuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "mint-endpointless-wrapper-srv-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             *authCtx.ProjectID,
		Name:                  pgtype.Text{String: "mint-endpointless-wrapper-srv", Valid: true},
		Slug:                  pgtype.Text{String: "mint-endpointless-wrapper-srv", Valid: true},
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.MustParse(wrapperIssuer.ID), Valid: true},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

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

	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(
		got.AccessToken,
		urn.NewToolset(toolset.ID).String(),
	)
	require.NoError(t, err)
	require.Equal(t, "http://0.0.0.0/mcp/"+toolset.McpSlug.String, claims.Issuer)
}
