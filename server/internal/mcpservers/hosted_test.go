package mcpservers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	toolsetsgen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostedmcptest"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type hostedFixture struct {
	ti        *testInstance
	toolsets  *toolsets.Service
	projectID uuid.UUID
	toolset   toolsetsrepo.Toolset
	slug      string
}

func newHostedFixture(t *testing.T) (context.Context, *hostedFixture) {
	t.Helper()

	ctx, ti := newTestService(t)
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	authzEngine := authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	toolsetsSvc := toolsets.NewService(logger, tracerProvider, guardianPolicy, ti.conn, ti.sessionManager, nil, authzEngine, audit.NewLogger(), nil, false)
	ti.service.WithHostedServerWriter(toolsetsSvc)

	created, err := toolsetsSvc.CreateToolset(ctx, &toolsetsgen.CreateToolsetPayload{Name: "Hosted wrapper", ToolUrns: []string{}})
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.NotNil(t, created.McpSlug)

	return ctx, &hostedFixture{ti: ti, toolsets: toolsetsSvc, projectID: *authCtx.ProjectID, toolset: toolset, slug: string(*created.McpSlug)}
}

func (f *hostedFixture) payload(visibility types.McpServerVisibility, mutate func(*gen.UpdateMcpServerPayload)) *gen.UpdateMcpServerPayload {
	payload := &gen.UpdateMcpServerPayload{ID: f.toolset.ID.String(), ToolsetID: new(f.toolset.ID.String()), Visibility: visibility}
	if mutate != nil {
		mutate(payload)
	}
	return payload
}

// requireProjected asserts the wrapper shares the toolset's name and tool variations group, keeps its single
// platform endpoint, and that the toolset hosting columns mirror it.
func (f *hostedFixture) requireProjected(t *testing.T, ctx context.Context) (toolsetsrepo.Toolset, mcpserversrepo.McpServer) {
	t.Helper()

	toolset, err := toolsetsrepo.New(f.ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	hostedmcptest.RequireMirrored(t, ctx, f.ti.conn, f.projectID, f.toolset.ID)
	server, err := mcpserversrepo.New(f.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.Equal(t, toolset.Name, server.Name.String)
	require.Equal(t, f.slug, server.Slug.String, "the server slug does not follow the name")
	require.Equal(t, toolset.ToolVariationsGroupID, server.ToolVariationsGroupID)
	endpoints, err := mcpendpointsrepo.New(f.ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: f.projectID, McpServerID: f.toolset.ID})
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	require.Equal(t, f.slug, endpoints[0].Slug)
	return toolset, server
}

func (f *hostedFixture) resolve(t *testing.T, ctx context.Context) mcpendpoints.ResolutionResult {
	t.Helper()

	input, err := mcpendpoints.FromContext(ctx, f.slug)
	require.NoError(t, err)
	result, err := mcpendpoints.Resolve(ctx, f.ti.conn, testenv.NewLogger(t), input)
	require.NoError(t, err)
	require.True(t, result.Found)
	return result
}

func TestUpdateMcpServerHostedWrapperWritesWrapper(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	groupID := seedToolVariationsGroup(t, ctx, f.ti.conn, f.projectID)

	view, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("public"), func(p *gen.UpdateMcpServerPayload) {
		p.Name = new("  Renamed hosted  ")
		p.ToolVariationsGroupID = new(groupID.String())
	}))
	require.NoError(t, err)
	require.Equal(t, "public", string(view.Visibility))
	require.NotNil(t, view.Toolset)
	require.Equal(t, f.toolset.ID.String(), view.Toolset.ID)
	require.Equal(t, conv.PtrEmpty(f.slug), view.PlatformEndpointSlug)
	require.NotNil(t, view.PrimaryEndpointID)

	toolset, server := f.requireProjected(t, ctx)
	require.Equal(t, "Renamed hosted", toolset.Name, "a rename renames the toolset too")
	require.Equal(t, "public", server.Visibility)
	require.Equal(t, groupID, server.ToolVariationsGroupID.UUID)
	require.True(t, f.resolve(t, ctx).Allowed)
	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, f.ti.conn, audit.ActionMcpServerUpdate, f.toolset.ID.String())
	require.NoError(t, err)
	require.Contains(t, string(record.AfterSnapshot), "Renamed hosted")

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("disabled"), nil))
	require.NoError(t, err)
	toolset, server = f.requireProjected(t, ctx)
	require.Equal(t, "disabled", server.Visibility)
	require.True(t, toolset.McpIsPublic, "disabling keeps the public flag the legacy columns re-enable with")
	require.False(t, server.ToolVariationsGroupID.Valid, "omitted tool variations group clears it")
	require.False(t, f.resolve(t, ctx).Allowed)
}

func TestUpdateMcpServerHostedWrapperStaysConsistentWithToolsetWrites(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)

	_, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("public"), func(p *gen.UpdateMcpServerPayload) { p.Name = new("Via server") }))
	require.NoError(t, err)
	f.requireProjected(t, ctx)

	// The toolset API still renames; its hosting fields are rejected.
	_, err = f.toolsets.UpdateToolset(ctx, &toolsetsgen.UpdateToolsetPayload{Slug: types.Slug(f.toolset.Slug), Name: new("Via toolset")})
	require.NoError(t, err)
	_, err = f.toolsets.UpdateToolset(ctx, &toolsetsgen.UpdateToolsetPayload{Slug: types.Slug(f.toolset.Slug), McpIsPublic: new(false)})
	require.ErrorIs(t, err, toolsets.ErrHostingFieldsMoved)
	toolset, server := f.requireProjected(t, ctx)
	require.Equal(t, "Via toolset", toolset.Name)
	require.Equal(t, "public", server.Visibility)
	require.True(t, f.resolve(t, ctx).Allowed)
}

func TestUpdateMcpServerHostedWrapperAppliesNetworkAccessMode(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)

	mode := types.NetworkAccessMode(networkaccess.ModePublicOnly)
	_, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.NetworkAccessMode = &mode }))
	require.NoError(t, err)
	_, server := f.requireProjected(t, ctx)
	require.Equal(t, networkaccess.ModePublicOnly, networkaccess.EffectiveForView(server.NetworkAccessMode))

	private := types.NetworkAccessMode(networkaccess.ModeDual)
	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.NetworkAccessMode = &private }))
	requireOopsCode(t, err, oops.CodeInvalid)
	f.requireProjected(t, ctx)
}

func TestUpdateMcpServerHostedWrapperRejectsShapeChanges(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	remoteID := seedRemoteMcpServer(t, ctx, f.ti.conn, f.projectID)
	_, before := f.requireProjected(t, ctx)

	_, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) {
		p.ToolsetID = nil
		p.RemoteMcpServerID = new(remoteID.String())
	}))
	requireOopsCode(t, err, oops.CodeInvalid)

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.EnvironmentID = new(uuid.NewString()) }))
	requireOopsCode(t, err, oops.CodeInvalid)

	_, after := f.requireProjected(t, ctx)
	require.Equal(t, before, after)
}

func TestUpdateMcpServerHostedWrapperRejectedWithoutWriter(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	f.ti.service.WithHostedServerWriter(nil)

	_, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.Name = new("Unwired") }))
	requireOopsCode(t, err, oops.CodeUnexpected)
	toolset, _ := f.requireProjected(t, ctx)
	require.Equal(t, f.toolset.Name, toolset.Name)
}

func TestUpdateMcpServerHostedWrapperLinksIssuerAndKeepsItWhenOmitted(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := usersessionsrepo.New(f.ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          f.projectID,
		OrganizationID:     pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:               "hosted-issuer-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: 24 * 60 * 60 * 1_000_000, Valid: true},
	})
	require.NoError(t, err)

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) {
		p.UserSessionIssuerID = new(issuer.ID.String())
	}))
	require.NoError(t, err)
	_, server := f.requireProjected(t, ctx)
	require.Equal(t, issuer.ID, server.UserSessionIssuerID.UUID)

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) { p.Name = new("Kept issuer") }))
	require.NoError(t, err)
	_, server = f.requireProjected(t, ctx)
	require.Equal(t, issuer.ID, server.UserSessionIssuerID.UUID)
	require.True(t, f.resolve(t, ctx).Allowed)
}

func TestUpdateMcpServerHostedWrapperRejectsPublicFlipWithExternalOAuth(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	_, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("public"), nil))
	require.NoError(t, err)
	metadata, err := oauthrepo.New(f.ti.conn).CreateExternalOAuthServerMetadata(ctx, oauthrepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: f.projectID, Slug: "hosted-oauth-" + uuid.NewString()[:8], Metadata: []byte(`{}`),
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(f.ti.conn).UpdateToolsetExternalOAuthServer(ctx, toolsetsrepo.UpdateToolsetExternalOAuthServerParams{
		ExternalOauthServerID: uuid.NullUUID{UUID: metadata.ID, Valid: true}, Slug: f.toolset.Slug, ProjectID: f.projectID,
	})
	require.NoError(t, err)

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), nil))
	requireOopsCode(t, err, oops.CodeInvalid)
	toolset, server := f.requireProjected(t, ctx)
	require.Equal(t, "public", server.Visibility)
	require.True(t, toolset.ExternalOauthServerID.Valid, "the external OAuth server is not silently detached")

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("disabled"), nil))
	require.NoError(t, err)
	_, server = f.requireProjected(t, ctx)
	require.Equal(t, "disabled", server.Visibility)
}

func TestUpdateMcpServerHostedWrapperFlipsVisibilityWithRetiredOAuthProxyLink(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := usersessionsrepo.New(f.ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          f.projectID,
		OrganizationID:     pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:               "proxy-link-issuer-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: 24 * 60 * 60 * 1_000_000, Valid: true},
	})
	require.NoError(t, err)
	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) {
		p.UserSessionIssuerID = new(issuer.ID.String())
	}))
	require.NoError(t, err)
	testenv.LinkRetiredOAuthProxy(t, ctx, f.ti.conn, f.projectID, f.toolset.ID)

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("public"), nil))
	require.NoError(t, err)
	toolset, server := f.requireProjected(t, ctx)
	require.True(t, toolset.McpIsPublic)
	require.Equal(t, "public", server.Visibility)
	require.Equal(t, issuer.ID, toolset.UserSessionIssuerID.UUID)

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), nil))
	require.NoError(t, err)
	toolset, server = f.requireProjected(t, ctx)
	require.False(t, toolset.McpIsPublic)
	require.Equal(t, "private", server.Visibility)
	require.Equal(t, issuer.ID, toolset.UserSessionIssuerID.UUID)
	require.True(t, f.resolve(t, ctx).Allowed)

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("disabled"), nil))
	require.NoError(t, err)
	toolset, server = f.requireProjected(t, ctx)
	require.Equal(t, "disabled", server.Visibility)
	require.False(t, hostedmcp.WithServerState(toolset, &server).McpIsPublic)

	tx := testenv.BeginTx(t, ctx, f.ti.conn)
	enabled, err := hostedmcp.Enable(ctx, tx, audit.NewLogger(), hostedmcp.SystemActor("re-enable-test"), toolset)
	require.NoError(t, err)
	require.True(t, enabled)
	require.NoError(t, tx.Commit(ctx))
	toolset, server = f.requireProjected(t, ctx)
	require.Equal(t, "private", server.Visibility, "the retired proxy link alone re-enables private")
	require.False(t, toolset.McpIsPublic)
}

func TestUpdateMcpServerHostedWrapperRejectsNameOverToolsetLimit(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)

	_, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("private"), func(p *gen.UpdateMcpServerPayload) {
		p.Name = new(strings.Repeat("n", 61))
	}))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateMcpServerHostedWrapperIgnoresDeletedToolsetWithSameSlug(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := toolsetsrepo.New(f.ti.conn)
	_, err := queries.DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: f.toolset.Slug, ProjectID: f.projectID})
	require.NoError(t, err)
	deletedID := f.toolset.ID

	recreated, err := f.toolsets.CreateToolset(ctx, &toolsetsgen.CreateToolsetPayload{Name: "Hosted wrapper", ToolUrns: []string{}})
	require.NoError(t, err)
	require.Equal(t, f.toolset.Slug, string(recreated.Slug))
	f.slug = string(*recreated.McpSlug)
	f.toolset, err = queries.GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: uuid.MustParse(recreated.ID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("public"), func(p *gen.UpdateMcpServerPayload) { p.Name = new("Live twin") }))
	require.NoError(t, err)
	toolset, _ := f.requireProjected(t, ctx)
	require.Equal(t, "Live twin", toolset.Name)

	deleted, err := queries.GetToolsetByIDIncludingDeletedFixture(ctx, toolsetsrepo.GetToolsetByIDIncludingDeletedFixtureParams{ID: deletedID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.Equal(t, "Hosted wrapper", deleted.Name, "the deleted twin is not rewritten")
}

func (f *hostedFixture) pluginMemberships(t *testing.T, ctx context.Context) int64 {
	t.Helper()

	count, err := pluginsrepo.New(f.ti.conn).CountLivePluginServersByMCPServerIDFixture(ctx, pluginsrepo.CountLivePluginServersByMCPServerIDFixtureParams{ProjectID: f.projectID, McpServerID: uuid.NullUUID{UUID: f.toolset.ID, Valid: true}})
	require.NoError(t, err)
	return count
}

func TestDeleteMcpServerHostedWrapperDisablesWrapper(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	groupID := seedToolVariationsGroup(t, ctx, f.ti.conn, f.projectID)
	_, err := f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("public"), func(p *gen.UpdateMcpServerPayload) {
		p.ToolVariationsGroupID = new(groupID.String())
	}))
	require.NoError(t, err)
	_, before := f.requireProjected(t, ctx)
	endpointsBefore, err := mcpendpointsrepo.New(f.ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: f.projectID, McpServerID: f.toolset.ID})
	require.NoError(t, err)
	membershipsBefore := f.pluginMemberships(t, ctx)
	toolsetUpdates, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	serverUpdates, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	serverDeletes, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionMcpServerDelete)
	require.NoError(t, err)
	endpointDeletes, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)

	require.NoError(t, f.ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{ID: f.toolset.ID.String()}))

	toolset, server := f.requireProjected(t, ctx)
	require.Equal(t, "disabled", server.Visibility)
	require.Equal(t, groupID, toolset.ToolVariationsGroupID.UUID, "the tool filtering group is kept")
	require.Equal(t, before.ID, server.ID, "the wrapper stays live")
	require.False(t, f.resolve(t, ctx).Allowed)
	endpointsAfter, err := mcpendpointsrepo.New(f.ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: f.projectID, McpServerID: f.toolset.ID})
	require.NoError(t, err)
	require.Equal(t, endpointsBefore[0].ID, endpointsAfter[0].ID, "the endpoint keeps its identity")
	require.Equal(t, membershipsBefore, f.pluginMemberships(t, ctx))

	f.requireAuditDelta(t, ctx, audit.ActionToolsetUpdate, toolsetUpdates, 1)
	f.requireAuditDelta(t, ctx, audit.ActionMcpServerUpdate, serverUpdates, 1)
	f.requireAuditDelta(t, ctx, audit.ActionMcpServerDelete, serverDeletes, 0)
	f.requireAuditDelta(t, ctx, audit.ActionMcpEndpointDelete, endpointDeletes, 0)

	view, err := f.ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{ID: new(f.toolset.ID.String())})
	require.NoError(t, err)
	require.Equal(t, "disabled", string(view.Visibility))

	// The same wrapper serves again once re-enabled.
	_, err = f.ti.service.UpdateMcpServer(ctx, f.payload(types.McpServerVisibility("public"), func(p *gen.UpdateMcpServerPayload) {
		p.ToolVariationsGroupID = new(groupID.String())
	}))
	require.NoError(t, err)
	_, server = f.requireProjected(t, ctx)
	require.Equal(t, before.ID, server.ID)
	require.Equal(t, "public", server.Visibility)
	require.True(t, f.resolve(t, ctx).Allowed)
}

func TestDeleteMcpServerHostedWrapperIsRepeatable(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)

	require.NoError(t, f.ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{ID: f.toolset.ID.String()}))
	_, first := f.requireProjected(t, ctx)
	require.Equal(t, "disabled", first.Visibility)
	require.False(t, first.Deleted)

	require.NoError(t, f.ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{ID: f.toolset.ID.String()}))
	_, second := f.requireProjected(t, ctx)
	require.Equal(t, first, second, "deleting an already disabled server changes nothing")
}

func TestDeleteMcpServerHostedWrapperRejectedWithoutWriter(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedFixture(t)
	f.ti.service.WithHostedServerWriter(nil)

	_, before := f.requireProjected(t, ctx)

	err := f.ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{ID: f.toolset.ID.String()})
	requireOopsCode(t, err, oops.CodeUnexpected)
	_, after := f.requireProjected(t, ctx)
	require.Equal(t, before, after)
}

func (f *hostedFixture) requireAuditDelta(t *testing.T, ctx context.Context, action audit.Action, before int64, delta int64) {
	t.Helper()

	after, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, action)
	require.NoError(t, err)
	require.Equal(t, before+delta, after, string(action))
}
