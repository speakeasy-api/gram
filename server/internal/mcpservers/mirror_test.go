package mcpservers_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// seedHostedToolset inserts a live, addressable hosted toolset the way the
// legacy dashboard flow left them: enabled, private, platform slug.
func seedHostedToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID) toolsetsrepo.Toolset {
	t.Helper()

	slug := "hosted-" + uuid.NewString()[:8]
	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         organizationID,
		ProjectID:              projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	return toolset
}

func createHostedServer(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID, visibility string) *types.McpServer {
	t.Helper()

	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "hosted " + toolsetID.String()[:8],
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   nil,
		ToolsetID:             conv.PtrEmpty(toolsetID.String()),
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility(visibility),
	})
	require.NoError(t, err)

	return created
}

func getToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, toolsetID uuid.UUID) toolsetsrepo.Toolset {
	t.Helper()

	toolset, err := toolsetsrepo.New(conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID:        toolsetID,
		ProjectID: projectID,
	})
	require.NoError(t, err)

	return toolset
}

func TestCreateMcpServer_ToolsetBacked_ProjectsVisibilityOntoToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	createHostedServer(t, ctx, ti, toolset.ID, "public")

	after := getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.True(t, after.McpEnabled)
	require.True(t, after.McpIsPublic)
}

func TestCreateMcpServer_SecondWrapperForToolsetConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	createHostedServer(t, ctx, ti, toolset.ID, "private")

	_, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "second wrapper",
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   nil,
		ToolsetID:             conv.PtrEmpty(toolset.ID.String()),
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Contains(t, err.Error(), "toolset already has an mcp server")
}

func TestUpdateMcpServer_ToolsetBacked_ProjectsVisibilityOntoToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	created := createHostedServer(t, ctx, ti, toolset.ID, "private")

	update := func(visibility string) {
		t.Helper()
		_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
			ID:                    created.ID,
			Name:                  nil,
			EnvironmentID:         nil,
			RemoteMcpServerID:     nil,
			TunneledMcpServerID:   nil,
			ToolsetID:             conv.PtrEmpty(toolset.ID.String()),
			UnproxiedMcpServerID:  nil,
			ToolVariationsGroupID: nil,
			Visibility:            types.McpServerVisibility(visibility),
		})
		require.NoError(t, err)
	}

	update("public")
	after := getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.True(t, after.McpEnabled)
	require.True(t, after.McpIsPublic)

	// Disabling clears mcp_is_public: the four toolset states fold onto three.
	update("disabled")
	after = getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.False(t, after.McpEnabled)
	require.False(t, after.McpIsPublic)

	update("private")
	after = getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.True(t, after.McpEnabled)
	require.False(t, after.McpIsPublic)
}

func TestDeleteMcpServer_ToolsetBacked_ClearsToolsetHosting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	created := createHostedServer(t, ctx, ti, toolset.ID, "private")
	serverID := uuid.MustParse(created.ID)

	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: serverID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            toolset.McpSlug.String,
	})
	require.NoError(t, err)

	endpointDeletesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	serverDeletesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
	}))

	// The toolset survives as a build artifact; only its hosting columns go.
	after := getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.False(t, after.McpSlug.Valid)
	require.False(t, after.McpEnabled)
	require.False(t, after.McpIsPublic)
	require.False(t, after.CustomDomainID.Valid)

	// Tombstoned, not hard-deleted: the rows survive with deleted set.
	wrapperDeleted, err := testrepo.New(ti.conn).GetMCPServerDeletedFixture(ctx, testrepo.GetMCPServerDeletedFixtureParams{ID: serverID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.True(t, wrapperDeleted)
	endpointCounts, err := testrepo.New(ti.conn).CountMCPEndpointsByDeletedFixture(ctx, testrepo.CountMCPEndpointsByDeletedFixtureParams{McpServerID: uuid.NullUUID{UUID: serverID, Valid: true}, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, int32(0), endpointCounts.Live)
	require.Equal(t, int32(1), endpointCounts.Tombstoned)

	endpointDeletesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	require.Equal(t, endpointDeletesBefore+1, endpointDeletesAfter)
	serverDeletesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)
	require.Equal(t, serverDeletesBefore+1, serverDeletesAfter)
}

func TestUpdateMcpServer_RejectsBackingToolsetChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	other := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	created := createHostedServer(t, ctx, ti, toolset.ID, "private")

	for name, payload := range map[string]gen.UpdateMcpServerPayload{
		"toolset":   {ToolsetID: conv.PtrEmpty(other.ID.String())},
		"remote":    {RemoteMcpServerID: conv.PtrEmpty(uuid.NewString())},
		"tunneled":  {TunneledMcpServerID: conv.PtrEmpty(uuid.NewString())},
		"unproxied": {UnproxiedMcpServerID: conv.PtrEmpty(uuid.NewString())},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			payload.ID = created.ID
			payload.Visibility = types.McpServerVisibility("public")
			_, err := ti.service.UpdateMcpServer(ctx, &payload)
			requireOopsCode(t, err, oops.CodeInvalid)
		})
	}
}

func TestUpdateMcpServer_ToolsetBacked_PrivateDetachesExternalOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	created := createHostedServer(t, ctx, ti, toolset.ID, "public")
	oauth, err := oauthrepo.New(ti.conn).CreateExternalOAuthServerMetadata(ctx, oauthrepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "ext-" + uuid.NewString()[:8],
		Metadata:  []byte(`{}`),
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).UpdateToolsetExternalOAuthServer(ctx, toolsetsrepo.UpdateToolsetExternalOAuthServerParams{
		ExternalOauthServerID: uuid.NullUUID{UUID: oauth.ID, Valid: true},
		Slug:                  toolset.Slug,
		ProjectID:             *authCtx.ProjectID,
	})
	require.NoError(t, err)
	detachesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID:         created.ID,
		ToolsetID:  conv.PtrEmpty(toolset.ID.String()),
		Visibility: types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	after := getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.False(t, after.McpIsPublic)
	require.False(t, after.ExternalOauthServerID.Valid, "a private server cannot keep an external OAuth server attached")
	detachesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, detachesBefore+1, detachesAfter)
}

// A hosted wrapper never owns its issuer: deleting it leaves the toolset's
// issuer, its sessions, and its external OAuth reference in place.
func TestDeleteMcpServer_ToolsetBacked_KeepsToolsetIssuerAndOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		OrganizationID:     pgtype.Text{String: "", Valid: false},
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           *authCtx.ProjectID,
	})
	require.NoError(t, err)
	created := createHostedServer(t, ctx, ti, toolset.ID, "public")
	oauth, err := oauthrepo.New(ti.conn).CreateExternalOAuthServerMetadata(ctx, oauthrepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "ext-" + uuid.NewString()[:8],
		Metadata:  []byte(`{}`),
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).UpdateToolsetExternalOAuthServer(ctx, toolsetsrepo.UpdateToolsetExternalOAuthServerParams{
		ExternalOauthServerID: uuid.NullUUID{UUID: oauth.ID, Valid: true},
		Slug:                  toolset.Slug,
		ProjectID:             *authCtx.ProjectID,
	})
	require.NoError(t, err)
	session, err := usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: issuer.ID,
		UserSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SubjectUrn:          urn.NewUserSubject(uuid.NewString()),
		Jti:                 "jti-" + uuid.NewString(),
		RefreshTokenHash:    "hash-" + uuid.NewString(),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: 0, Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: 0, Valid: true},
		ToolSelection:       nil,
	})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
	}))

	_, err = usersessionsrepo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID:             issuer.ID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err, "the toolset's issuer must survive its wrapper")
	_, err = usersessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{
		UserSessionIssuerID: issuer.ID,
		Jti:                 session.Jti,
	})
	require.NoError(t, err, "sessions minted against the toolset's issuer must survive")

	after := getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.False(t, after.McpEnabled)
	require.False(t, after.UserSessionIssuerID.Valid)
	require.Equal(t, uuid.NullUUID{UUID: oauth.ID, Valid: true}, after.ExternalOauthServerID, "hosting removal does not detach external OAuth")
}

// Hosted servers are attached to the Default plugin toolset-keyed until
// AIS-638; enabling through the wrapper must not add a server-keyed twin.
func TestUpdateMcpServer_ToolsetBacked_EnableSkipsDefaultPluginAttach(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginsQueries := pluginsrepo.New(ti.conn)
	defaultPlugin, err := pluginsQueries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	_, err = pluginsQueries.AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID:    defaultPlugin.ID,
		ToolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
		McpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		DisplayName: toolset.Name,
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	created := createHostedServer(t, ctx, ti, toolset.ID, "disabled")
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: uuid.MustParse(created.ID), Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            toolset.McpSlug.String,
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    created.ID,
		Name:                  nil,
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   nil,
		ToolsetID:             conv.PtrEmpty(toolset.ID.String()),
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, uuid.NullUUID{UUID: toolset.ID, Valid: true}, servers[0].ToolsetID)
	require.False(t, servers[0].McpServerID.Valid)
}

// Disabling through the wrapper keeps external OAuth attached, as the toolset
// path's disable does; only an enabled public->private flip detaches it.
func TestUpdateMcpServer_ToolsetBacked_DisableKeepsExternalOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	created := createHostedServer(t, ctx, ti, toolset.ID, "public")
	oauth, err := oauthrepo.New(ti.conn).CreateExternalOAuthServerMetadata(ctx, oauthrepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "ext-" + uuid.NewString()[:8],
		Metadata:  []byte(`{}`),
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).UpdateToolsetExternalOAuthServer(ctx, toolsetsrepo.UpdateToolsetExternalOAuthServerParams{
		ExternalOauthServerID: uuid.NullUUID{UUID: oauth.ID, Valid: true},
		Slug:                  toolset.Slug,
		ProjectID:             *authCtx.ProjectID,
	})
	require.NoError(t, err)

	update := func(visibility string) {
		t.Helper()
		_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
			ID:                    created.ID,
			Name:                  nil,
			EnvironmentID:         nil,
			RemoteMcpServerID:     nil,
			TunneledMcpServerID:   nil,
			ToolsetID:             conv.PtrEmpty(toolset.ID.String()),
			UnproxiedMcpServerID:  nil,
			ToolVariationsGroupID: nil,
			Visibility:            types.McpServerVisibility(visibility),
		})
		require.NoError(t, err)
	}

	update("disabled")
	after := getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.False(t, after.McpEnabled)
	require.Equal(t, uuid.NullUUID{UUID: oauth.ID, Valid: true}, after.ExternalOauthServerID)

	update("public")
	update("private")
	after = getToolset(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.True(t, after.McpEnabled)
	require.False(t, after.ExternalOauthServerID.Valid)
}

// Adopting a toolset issuer derives remote_session_issuer_id at creation, as a
// later issuer change through the mirror would.
func TestCreateMcpServer_ToolsetBacked_DerivesRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		OrganizationID:     pgtype.Text{String: "", Valid: false},
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           *authCtx.ProjectID,
	})
	require.NoError(t, err)
	remoteIssuerID := seedBoundRemoteSessionIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, issuer.ID)

	created := createHostedServer(t, ctx, ti, toolset.ID, "private")

	wrapper, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: issuer.ID, Valid: true}, wrapper.UserSessionIssuerID)
	require.Equal(t, uuid.NullUUID{UUID: remoteIssuerID, Valid: true}, wrapper.RemoteSessionIssuerID)
}

// seedBoundRemoteSessionIssuer creates a remote session issuer with one client
// bound to the given user issuer, the shape the resync derives from.
func seedBoundRemoteSessionIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID, userIssuerID uuid.UUID) uuid.UUID {
	t.Helper()

	q := remotesessionsrepo.New(conn)
	suffix := uuid.NewString()[:8]
	issuer, err := q.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(projectID),
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              "rsi-" + suffix,
		Issuer:                            "https://issuer-" + suffix + ".example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://issuer-" + suffix + ".example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://issuer-" + suffix + ".example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	})
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGTextEmpty(organizationID),
		RemoteSessionIssuerID: issuer.ID,
		ClientID:              "client-" + suffix,
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now()),
	})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuerID,
	}))
	return issuer.ID
}
