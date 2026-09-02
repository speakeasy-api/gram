package toolsets_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	mcpserversgen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	variationsrepo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

func mirroredWrapper(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, toolsetID uuid.UUID) mcpserversrepo.McpServer {
	t.Helper()

	wrapper, err := mcpserversrepo.New(conn).GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{
		ToolsetID: toolsetID,
		ProjectID: projectID,
	})
	require.NoError(t, err)

	return wrapper
}

func wrapperEndpoints(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, serverID uuid.UUID) []mcpendpointsrepo.McpEndpoint {
	t.Helper()

	endpoints, err := mcpendpointsrepo.New(conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID:   projectID,
		McpServerID: serverID,
	})
	require.NoError(t, err)

	return endpoints
}

func toolsetRow(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, toolsetID uuid.UUID) toolsetsrepo.Toolset {
	t.Helper()

	toolset, err := toolsetsrepo.New(conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID:        toolsetID,
		ProjectID: projectID,
	})
	require.NoError(t, err)

	return toolset
}

func updateToolsetFlags(t *testing.T, ctx context.Context, ti *testInstance, slug types.Slug, enabled, public *bool) {
	t.Helper()

	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Slug:                   slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             enabled,
		McpIsPublic:            public,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
}

func TestToolsetsService_CreateToolset_MirrorsWrapperAndPlatformEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverCreatesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerCreate)
	require.NoError(t, err)
	endpointCreatesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointCreate)
	require.NoError(t, err)

	created := createMinimalPrivateToolset(t, ctx, ti, "Mirrored Toolset")
	require.NotNil(t, created.McpSlug)

	wrapper := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, uuid.MustParse(created.ID))
	require.Equal(t, mcpservers.VisibilityPrivate, wrapper.Visibility, "the first toolset is auto-enabled, so its wrapper is private")
	require.Equal(t, created.Name, conv.FromPGTextOrEmpty[string](wrapper.Name))

	endpoints := wrapperEndpoints(t, ctx, ti.conn, *authCtx.ProjectID, wrapper.ID)
	require.Len(t, endpoints, 1)
	require.Equal(t, string(*created.McpSlug), endpoints[0].Slug)
	require.False(t, endpoints[0].CustomDomainID.Valid)

	serverCreatesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerCreate)
	require.NoError(t, err)
	require.Equal(t, serverCreatesBefore+1, serverCreatesAfter)
	endpointCreatesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointCreate)
	require.NoError(t, err)
	require.Equal(t, endpointCreatesBefore+1, endpointCreatesAfter)
}

// The four (mcp_enabled, mcp_is_public) states fold onto the wrapper's three
// visibilities, and a disabled toolset stays disabled whatever its public flag.
func TestToolsetsService_UpdateToolset_VisibilityRoundTripsOntoWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created := createMinimalPrivateToolset(t, ctx, ti, "Visibility Toolset")
	toolsetID := uuid.MustParse(created.ID)

	updateToolsetFlags(t, ctx, ti, created.Slug, nil, new(true))
	require.Equal(t, mcpservers.VisibilityPublic, mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID).Visibility)

	updateToolsetFlags(t, ctx, ti, created.Slug, new(false), nil)
	require.Equal(t, mcpservers.VisibilityDisabled, mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID).Visibility)

	updateToolsetFlags(t, ctx, ti, created.Slug, new(true), nil)
	require.Equal(t, mcpservers.VisibilityPublic, mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID).Visibility)

	updateToolsetFlags(t, ctx, ti, created.Slug, nil, new(false))
	require.Equal(t, mcpservers.VisibilityPrivate, mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID).Visibility)
}

func TestToolsetsService_UpdateToolset_RekeysPrimaryEndpointOnSlugChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created := createMinimalPrivateToolset(t, ctx, ti, "Rekey Toolset")
	toolsetID := uuid.MustParse(created.ID)
	wrapper := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID)
	before := wrapperEndpoints(t, ctx, ti.conn, *authCtx.ProjectID, wrapper.ID)
	require.Len(t, before, 1)

	renamed := authCtx.OrganizationSlug + "-rekeyed"
	_, err := ti.service.UpdateToolset(ctx, updateMcpSlugPayload(created.Slug, renamed))
	require.NoError(t, err)

	after := wrapperEndpoints(t, ctx, ti.conn, *authCtx.ProjectID, wrapper.ID)
	require.Len(t, after, 1)
	require.Equal(t, before[0].ID, after[0].ID, "the primary endpoint is re-keyed, not replaced")
	require.Equal(t, renamed, after[0].Slug)
}

func TestToolsetsService_UpdateToolset_MovesEndpointOntoCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created := createMinimalPrivateToolset(t, ctx, ti, "Domain Toolset")
	toolsetID := uuid.MustParse(created.ID)
	wrapper := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID)

	domainID := seedActiveCustomDomain(t, ctx, ti.conn, authCtx.ActiveOrganizationID)

	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                conv.PtrEmpty(types.Slug("partners")),
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         conv.PtrEmpty(domainID.String()),
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	endpoints := wrapperEndpoints(t, ctx, ti.conn, *authCtx.ProjectID, wrapper.ID)
	require.Len(t, endpoints, 1)
	require.Equal(t, "partners", endpoints[0].Slug)
	require.Equal(t, uuid.NullUUID{UUID: domainID, Valid: true}, endpoints[0].CustomDomainID)
}

func TestToolsetsService_DeleteToolset_TombstonesWrapperAndEndpoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created := createMinimalPrivateToolset(t, ctx, ti, "Doomed Toolset")
	toolsetID := uuid.MustParse(created.ID)
	wrapper := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID)

	endpointDeletesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	serverDeletesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	_, err = mcpserversrepo.New(ti.conn).GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{
		ToolsetID: toolsetID,
		ProjectID: *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Empty(t, wrapperEndpoints(t, ctx, ti.conn, *authCtx.ProjectID, wrapper.ID))

	endpointDeletesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	require.Equal(t, endpointDeletesBefore+1, endpointDeletesAfter)
	serverDeletesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)
	require.Equal(t, serverDeletesBefore+1, serverDeletesAfter)
}

func TestToolsetsService_CloneToolset_MirrorsDisabledWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created := createMinimalPrivateToolset(t, ctx, ti, "Clone Source")
	cloned, err := ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, cloned.McpSlug)

	wrapper := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, uuid.MustParse(cloned.ID))
	require.Equal(t, mcpservers.VisibilityDisabled, wrapper.Visibility)
	endpoints := wrapperEndpoints(t, ctx, ti.conn, *authCtx.ProjectID, wrapper.ID)
	require.Len(t, endpoints, 1)
	require.Equal(t, string(*cloned.McpSlug), endpoints[0].Slug)
}

func TestToolsetsService_SetUserSessionIssuer_MirrorsWrapperIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created := createMinimalPrivateToolset(t, ctx, ti, "Issuer Toolset")
	toolsetID := uuid.MustParse(created.ID)

	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		OrganizationID:     pgtype.Text{String: "", Valid: false},
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	remoteIssuerID := seedBoundRemoteSessionIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, issuer.ID)

	_, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		Slug:                created.Slug,
		UserSessionIssuerID: conv.PtrEmpty(issuer.ID.String()),
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	wrapper := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID)
	require.Equal(t, uuid.NullUUID{UUID: issuer.ID, Valid: true}, wrapper.UserSessionIssuerID)
	require.Equal(t, uuid.NullUUID{UUID: remoteIssuerID, Valid: true}, wrapper.RemoteSessionIssuerID)

	// Clearing the issuer must clear the derived remote issuer with it: the
	// resync only reaches servers still on the old issuer, never this one.
	_, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		Slug:                created.Slug,
		UserSessionIssuerID: nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	wrapper = mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID)
	require.False(t, wrapper.UserSessionIssuerID.Valid)
	require.False(t, wrapper.RemoteSessionIssuerID.Valid)
}

// seedBoundRemoteSessionIssuer creates a remote session issuer with one client
// attached to the given user session issuer, so the wrapper derives it.
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

func TestToolsetsService_SetToolVariationsGroup_MirrorsWrapperGroup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created := createMinimalPrivateToolset(t, ctx, ti, "Variations Toolset")
	toolsetID := uuid.MustParse(created.ID)

	groupID, err := variationsrepo.New(ti.conn).InitGlobalToolVariationsGroup(ctx, variationsrepo.InitGlobalToolVariationsGroupParams{
		ProjectID:   *authCtx.ProjectID,
		Name:        "Global tool variations",
		Description: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	_, err = ti.service.SetToolVariationsGroup(ctx, &gen.SetToolVariationsGroupPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		Slug:                  created.Slug,
		ToolVariationsGroupID: conv.PtrEmpty(groupID.String()),
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: groupID, Valid: true}, mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID).ToolVariationsGroupID)
}

// A wrapper created by hand before the mirror existed has no endpoint; the
// next toolset write gives it one instead of failing.
func TestToolsetsService_UpdateToolset_AdoptsEndpointlessWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "adopted-" + uuid.NewString()[:8]
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(authCtx.OrganizationSlug + "-" + slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	wrapper, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    serverID,
		ProjectID:             *authCtx.ProjectID,
		Name:                  conv.ToPGText("hand-made wrapper"),
		Slug:                  conv.ToPGText("hand-made-" + uuid.NewString()[:8]),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            mcpservers.VisibilityDisabled,
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Slug:                   types.Slug(toolset.Slug),
		Name:                   nil,
		Description:            conv.PtrEmpty("adopted"),
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	adopted := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.Equal(t, wrapper.ID, adopted.ID)
	require.Equal(t, mcpservers.VisibilityPrivate, adopted.Visibility, "the wrapper is reconciled to the toolset's enabled, private flags")
	require.Equal(t, "hand-made wrapper", conv.FromPGTextOrEmpty[string](adopted.Name), "adoption keeps the wrapper's own name; only a toolset rename moves it")

	endpoints := wrapperEndpoints(t, ctx, ti.conn, *authCtx.ProjectID, wrapper.ID)
	require.Len(t, endpoints, 1)
	require.Equal(t, toolset.McpSlug.String, endpoints[0].Slug)
}

func newMirrorMcpServersService(t *testing.T, ti *testInstance) *mcpservers.Service {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	dispositions := mcpservers.NewToolDispositionCache(logger, ti.conn, cache.NewRedisCacheAdapter(redisClient))
	revoker := remotesessions.NewUpstreamRevoker(logger, tracerProvider, testenv.NewMeterProvider(t), ti.conn, testenv.NewEncryptionClient(t), guardianPolicy)

	return mcpservers.NewService(logger, tracerProvider, ti.conn, ti.sessionManager, authzEngine, audit.NewLogger(), nil, dispositions, false, ti.assets, revoker)
}

// Writes from both sides take the toolset row lock first, so however they
// interleave the toolset flags and the wrapper visibility end up agreeing.
func TestToolsetsService_ConcurrentToolsetAndWrapperWritesSerialize(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serversSvc := newMirrorMcpServersService(t, ti)

	created := createMinimalPrivateToolset(t, ctx, ti, "Contended Toolset")
	toolsetID := uuid.MustParse(created.ID)
	wrapper := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID)

	const rounds = 8
	errs := make(chan error, 2*rounds)
	var wg sync.WaitGroup
	for i := range rounds {
		public := i%2 == 0
		visibility := mcpservers.VisibilityPrivate
		if i%3 == 0 {
			visibility = mcpservers.VisibilityPublic
		}
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
				SessionToken:           nil,
				ApikeyToken:            nil,
				Slug:                   created.Slug,
				Name:                   nil,
				Description:            nil,
				DefaultEnvironmentSlug: nil,
				ToolUrns:               nil,
				ResourceUrns:           nil,
				PromptTemplateNames:    nil,
				McpSlug:                nil,
				McpEnabled:             nil,
				McpIsPublic:            new(public),
				CustomDomainID:         nil,
				ProjectSlugInput:       nil,
			})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := serversSvc.UpdateMcpServer(ctx, &mcpserversgen.UpdateMcpServerPayload{
				SessionToken:          nil,
				ApikeyToken:           nil,
				ProjectSlugInput:      nil,
				ID:                    wrapper.ID.String(),
				Name:                  nil,
				EnvironmentID:         nil,
				RemoteMcpServerID:     nil,
				TunneledMcpServerID:   nil,
				ToolsetID:             conv.PtrEmpty(toolsetID.String()),
				UnproxiedMcpServerID:  nil,
				ToolVariationsGroupID: nil,
				Visibility:            types.McpServerVisibility(visibility),
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	toolset := toolsetRow(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID)
	final := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID)
	require.Equal(t, hostedmcp.VisibilityForToolset(toolset.McpEnabled, toolset.McpIsPublic), final.Visibility)
}

func seedActiveCustomDomain(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	domain, err := cdrepo.New(conn).CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  organizationID,
		Domain:          "mirror-" + uuid.NewString()[:8] + ".example.com",
		IngressName:     conv.ToPGText("ingress-mirror"),
		CertSecretName:  conv.ToPGText("cert-mirror"),
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	_, err = cdrepo.New(conn).SetCustomDomainVerified(ctx, domain.ID)
	require.NoError(t, err)
	activated, err := cdrepo.New(conn).ActivateVerifiedCustomDomain(ctx, cdrepo.ActivateVerifiedCustomDomainParams{
		IngressName:     conv.ToPGText("ingress-mirror"),
		CertSecretName:  conv.ToPGText("cert-mirror"),
		ProvisionerKind: "ingress",
		ID:              domain.ID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, activated)

	return domain.ID
}

func updateToolsetAddress(t *testing.T, ctx context.Context, ti *testInstance, slug types.Slug, mcpSlug string, domainID *uuid.UUID) {
	t.Helper()

	var domain *string
	if domainID != nil {
		domain = conv.PtrEmpty(domainID.String())
	}
	var address *types.Slug
	if mcpSlug != "" {
		address = conv.PtrEmpty(types.Slug(mcpSlug))
	}
	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		Slug:           slug,
		McpSlug:        address,
		CustomDomainID: domain,
	})
	require.NoError(t, err)
}

func TestToolsetsService_UpdateToolset_KeepsWrapperRename(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created := createMinimalPrivateToolset(t, ctx, ti, "Rename Source")
	toolsetID := uuid.MustParse(created.ID)
	wrapper := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID)

	renamedSlug, err := mcpservers.ComputeServerSlug("Support (prod)", wrapper.ID)
	require.NoError(t, err)
	_, err = mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		Name:                  conv.ToPGText("Support (prod)"),
		Slug:                  conv.ToPGText(renamedSlug),
		EnvironmentID:         wrapper.EnvironmentID,
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     wrapper.RemoteMcpServerID,
		TunneledMcpServerID:   wrapper.TunneledMcpServerID,
		ToolsetID:             wrapper.ToolsetID,
		UnproxiedMcpServerID:  wrapper.UnproxiedMcpServerID,
		ToolVariationsGroupID: wrapper.ToolVariationsGroupID,
		Visibility:            wrapper.Visibility,
		ID:                    wrapper.ID,
		ProjectID:             *authCtx.ProjectID,
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, Description: conv.PtrEmpty("touched")})
	require.NoError(t, err)
	require.Equal(t, "Support (prod)", conv.FromPGTextOrEmpty[string](mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID).Name), "a toolset write that leaves the name alone keeps the wrapper's own name")

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, Name: conv.PtrEmpty("Renamed Toolset")})
	require.NoError(t, err)
	require.Equal(t, "Renamed Toolset", conv.FromPGTextOrEmpty[string](mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID).Name))
}

func TestToolsetsService_UpdateToolset_DeadDomainCreatesNoEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	domainID := seedActiveCustomDomain(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	slug := "dead-domain-" + uuid.NewString()[:8]
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           slug,
		Slug:           slug,
		McpSlug:        conv.ToPGText("orphaned"),
		McpEnabled:     true,
	})
	require.NoError(t, err)
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetCustomDomain(ctx, toolsetsrepo.SetToolsetCustomDomainParams{
		CustomDomainID: uuid.NullUUID{UUID: domainID, Valid: true},
		Slug:           slug,
		ProjectID:      *authCtx.ProjectID,
	}))
	require.NoError(t, cdrepo.New(ti.conn).DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID))

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: types.Slug(slug), Description: conv.PtrEmpty("first write after deploy")})
	require.NoError(t, err)

	wrapper := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.Empty(t, wrapperEndpoints(t, ctx, ti.conn, *authCtx.ProjectID, wrapper.ID), "a dead domain expresses no address")
}

// seedTwinWrapper returns a toolset addressed on its custom domain whose wrapper
// also carries a platform-scope twin at the same slug, the shape the backfill's
// alias allowlist produces.
func seedTwinWrapper(t *testing.T, ctx context.Context, ti *testInstance) (*types.Toolset, mcpserversrepo.McpServer, uuid.UUID) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created := createMinimalPrivateToolset(t, ctx, ti, "Twin Toolset")
	domainID := seedActiveCustomDomain(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	updateToolsetAddress(t, ctx, ti, created.Slug, "twin", &domainID)
	wrapper := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, uuid.MustParse(created.ID))
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:   *authCtx.ProjectID,
		McpServerID: uuid.NullUUID{UUID: wrapper.ID, Valid: true},
		Slug:        "twin",
	})
	require.NoError(t, err)

	return created, wrapper, domainID
}

func TestToolsetsService_UpdateToolset_RekeysAliasTwin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, wrapper, domainID := seedTwinWrapper(t, ctx, ti)

	updateToolsetAddress(t, ctx, ti, created.Slug, "twin-renamed", nil)

	endpoints := wrapperEndpoints(t, ctx, ti.conn, *authCtx.ProjectID, wrapper.ID)
	require.Len(t, endpoints, 2)
	scopes := map[uuid.NullUUID]string{}
	for _, endpoint := range endpoints {
		scopes[endpoint.CustomDomainID] = endpoint.Slug
	}
	require.Equal(t, map[uuid.NullUUID]string{
		{UUID: domainID, Valid: true}:  "twin-renamed",
		{UUID: uuid.Nil, Valid: false}: "twin-renamed",
	}, scopes, "the twin follows the rename within its own scope")
}

func TestToolsetsService_UpdateToolset_ScopeChangeRetiresOldScopeHolder(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created := createMinimalPrivateToolset(t, ctx, ti, "Scope Toolset")
	toolsetID := uuid.MustParse(created.ID)
	domainID := seedActiveCustomDomain(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	wrapper := mirroredWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolsetID)
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: domainID, Valid: true},
		McpServerID:    uuid.NullUUID{UUID: wrapper.ID, Valid: true},
		Slug:           string(*created.McpSlug),
	})
	require.NoError(t, err)

	updateToolsetAddress(t, ctx, ti, created.Slug, "", &domainID)

	endpoints := wrapperEndpoints(t, ctx, ti.conn, *authCtx.ProjectID, wrapper.ID)
	require.Len(t, endpoints, 1, "the platform endpoint the toolset left is retired, not duplicated")
	require.Equal(t, uuid.NullUUID{UUID: domainID, Valid: true}, endpoints[0].CustomDomainID)
	require.Equal(t, endpoints[0], *mcpendpoints.PrimaryEndpoint(endpoints), "the survivor is what a later endpoint write mirrors back")
}
