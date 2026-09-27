package mcpservers_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestCreateMcpServer_RemoteMcpBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "test mcp server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotEmpty(t, result.ID)
	require.NotEmpty(t, result.ProjectID)
	require.NotNil(t, result.Name)
	require.Equal(t, "test mcp server", *result.Name)
	require.NotNil(t, result.Slug)
	require.NotEmpty(t, *result.Slug)
	require.NotNil(t, result.RemoteMcpServerID)
	require.Equal(t, serverID, *result.RemoteMcpServerID)
	require.NotNil(t, result.UserSessionIssuerID)
	require.NotEmpty(t, *result.UserSessionIssuerID)
	require.Nil(t, result.ToolsetID)
	require.Equal(t, types.McpServerVisibility("disabled"), result.Visibility)
	require.Equal(t, types.NetworkAccessMode("public_only"), result.NetworkAccessMode)

	stored, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID: uuid.MustParse(result.ID), ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.False(t, stored.NetworkAccessMode.Valid)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateMcpServer_NonPublicModesFailClosed(t *testing.T) {
	t.Parallel()

	for _, requested := range []types.NetworkAccessMode{"dual", "private_only"} {
		t.Run(string(requested), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

			_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
				Name: "fail closed " + string(requested), RemoteMcpServerID: &remoteID,
				Visibility: types.McpServerVisibility("disabled"), NetworkAccessMode: &requested,
			})
			requireOopsCode(t, err, oops.CodeForbidden)
		})
	}
}

func TestCreateMcpServer_UnproxiedRejectsNonPublicMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withStaffEmail(t, ctx)
	unproxiedID := seedUnproxiedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	requested := types.NetworkAccessMode("dual")

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name: "unproxied private", UnproxiedMcpServerID: &unproxiedID,
		Visibility: types.McpServerVisibility("private"), NetworkAccessMode: &requested,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpServer_RejectsEmptyName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateMcpServer_RejectsWhitespaceName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "   ",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateMcpServer_MissingBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "test mcp server",
		EnvironmentID:     nil,
		RemoteMcpServerID: nil,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpServer_BothBackends(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	toolsetID := "00000000-0000-0000-0000-000000000001"

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "test mcp server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         &toolsetID,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

// Remote-backed create without an issuer mints one in the same transaction —
// the server can never exist without its lifetime issuer.
func TestCreateMcpServer_RemoteMintsUserSessionIssuerWhenOmitted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	result, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "remote without issuer",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, result.UserSessionIssuerID)
	require.NotEmpty(t, *result.UserSessionIssuerID)
	require.NotNil(t, result.Slug)

	issuerID, err := uuid.Parse(*result.UserSessionIssuerID)
	require.NoError(t, err)
	issuer, err := usersessionsrepo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID:        issuerID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.LessOrEqual(t, len(issuer.Slug), 40)
	require.True(t, strings.HasPrefix(issuer.Slug, *result.Slug+"-"))
}

func TestCreateMcpServer_AttachesOrganizationUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:                         "shared-workforce",
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	issuerID := issuer.ID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:                "organization authenticated server",
		RemoteMcpServerID:   &remoteID,
		UserSessionIssuerID: &issuerID,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.Equal(t, issuerID, *created.UserSessionIssuerID)
}

func TestCreateMcpServer_ResolvesRemoteIssuerForSelectedOrganizationIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userIssuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                         "shared-with-upstream",
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	remoteSessions := remotesessionsrepo.New(ti.conn)
	remoteIssuer, err := remoteSessions.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              "upstream-identity",
		Issuer:                            "https://identity.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://identity.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://identity.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	client, err := remoteSessions.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID: remoteIssuer.ID,
		ClientID:              "selected-org-issuer-client",
	})
	require.NoError(t, err)
	require.NoError(t, remoteSessions.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuer.ID,
	}))

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	userIssuerID := userIssuer.ID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:                "organization authenticated server",
		RemoteMcpServerID:   &remoteID,
		UserSessionIssuerID: &userIssuerID,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	stored, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        uuid.MustParse(created.ID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, remoteIssuer.ID, stored.RemoteSessionIssuerID.UUID)
	require.True(t, stored.RemoteSessionIssuerID.Valid)
}

func TestCreateMcpServer_RejectsForeignOrganizationUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	foreignOrganizationID := "org-" + uuid.NewString()
	require.NoError(t, organizationsrepo.New(ti.conn).CreateOrganizationMetadata(ctx, organizationsrepo.CreateOrganizationMetadataParams{
		ID:   foreignOrganizationID,
		Name: "Foreign Organization",
		Slug: "foreign-" + uuid.NewString(),
	}))

	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               pgtype.Text{String: foreignOrganizationID, Valid: true},
		Slug:                         "foreign-workforce",
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	issuerID := issuer.ID.String()
	_, err = ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:                "foreign issuer server",
		RemoteMcpServerID:   &remoteID,
		UserSessionIssuerID: &issuerID,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestCreateMcpServer_TunneledMintsUserSessionIssuerWhenOmitted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunneledServerID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	result, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "tunneled without issuer",
		EnvironmentID:       nil,
		TunneledMcpServerID: &tunneledServerID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.NotNil(t, result.UserSessionIssuerID)
	require.NotEmpty(t, *result.UserSessionIssuerID)
}

func TestCreateMcpServer_TunneledMcpRejectsPublicVisibility(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunneledServerID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "public tunneled mcp server",
		EnvironmentID:       nil,
		TunneledMcpServerID: &tunneledServerID,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("public"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpServer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	// Grant only read, attempt create (requires write).
	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "test mcp server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &serverID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateMcpServer_TunneledMcpPublicAllowedWithConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunneledServerID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	enableTunneledPublicConsent(t, ctx, ti.conn, *authCtx.ProjectID, tunneledServerID)

	tunneledServerIDStr := tunneledServerID.String()
	result, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "public tunneled mcp server with consent",
		EnvironmentID:       nil,
		TunneledMcpServerID: &tunneledServerIDStr,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	require.Equal(t, types.McpServerVisibility("public"), result.Visibility)
}

func TestCreateMcpServer_UnproxiedBackendRejectsNonStaff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// newTestService seeds the default mock user (a non-Speakeasy domain), so
	// attaching an unproxied backend must be rejected without staff override.
	serverID := seedUnproxiedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Name:                 "test unproxied mcp server",
		EnvironmentID:        nil,
		UnproxiedMcpServerID: &serverID,
		ToolsetID:            nil,
		Visibility:           types.McpServerVisibility("private"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateMcpServer_UnproxiedBackendAllowsStaff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedUnproxiedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	ctx = withStaffEmail(t, ctx)

	result, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Name:                 "test unproxied mcp server",
		EnvironmentID:        nil,
		UnproxiedMcpServerID: &serverID,
		ToolsetID:            nil,
		Visibility:           types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.NotNil(t, result.UnproxiedMcpServerID)
	require.Equal(t, serverID, *result.UnproxiedMcpServerID)
	require.Nil(t, result.UserSessionIssuerID)
}
