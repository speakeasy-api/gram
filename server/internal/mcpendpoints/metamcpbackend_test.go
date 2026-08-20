package mcpendpoints_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// seedMetaMcpServer inserts a meta_mcp_servers row in the given project so
// backend tests have a valid meta_mcp_server_id FK.
func seedMetaMcpServer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           projectID,
		Name:                "endpoint-backed meta",
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	return meta.ID
}

func TestCreateMcpEndpoint_MetaBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	metaID := seedMetaMcpServer(t, ctx, ti, *authCtx.ProjectID)

	result, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      nil,
		MetaMcpServerID:  conv.PtrEmpty(metaID.String()),
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-meta-endpoint"),
	})
	require.NoError(t, err)
	require.Nil(t, result.McpServerID)
	require.NotNil(t, result.MetaMcpServerID)
	require.Equal(t, metaID.String(), *result.MetaMcpServerID)
}

func TestCreateMcpEndpoint_RejectsZeroBackends(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      nil,
		MetaMcpServerID:  nil,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-no-backend"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpEndpoint_RejectsTwoBackends(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	metaID := seedMetaMcpServer(t, ctx, ti, *authCtx.ProjectID)

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      conv.PtrEmpty(mcpServerID.String()),
		MetaMcpServerID:  conv.PtrEmpty(metaID.String()),
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-two-backends"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpEndpoint_RejectsForeignProjectMeta(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "other-" + uuid.New().String()[:8]
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	foreignMetaID := seedMetaMcpServer(t, ctx, ti, otherProject.ID)

	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      nil,
		MetaMcpServerID:  conv.PtrEmpty(foreignMetaID.String()),
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-foreign-meta"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpEndpoint_RejectsTombstonedMeta(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	metaID := seedMetaMcpServer(t, ctx, ti, *authCtx.ProjectID)

	_, err := metamcprepo.New(ti.conn).DeleteMetaMCPServer(ctx, metamcprepo.DeleteMetaMCPServerParams{
		ID:             metaID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      nil,
		MetaMcpServerID:  conv.PtrEmpty(metaID.String()),
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-tombstoned-meta"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpEndpoint_SwitchesBackends(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	metaID := seedMetaMcpServer(t, ctx, ti, *authCtx.ProjectID)

	slug := authCtx.OrganizationSlug + "-switching"
	created, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      conv.PtrEmpty(mcpServerID.String()),
		MetaMcpServerID:  nil,
		Slug:             types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)

	toMeta, err := ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		CustomDomainID:   nil,
		McpServerID:      nil,
		MetaMcpServerID:  conv.PtrEmpty(metaID.String()),
		Slug:             types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)
	require.Nil(t, toMeta.McpServerID)
	require.NotNil(t, toMeta.MetaMcpServerID)
	require.Equal(t, metaID.String(), *toMeta.MetaMcpServerID)

	backToGeneric, err := ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		CustomDomainID:   nil,
		McpServerID:      conv.PtrEmpty(mcpServerID.String()),
		MetaMcpServerID:  nil,
		Slug:             types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)
	require.Nil(t, backToGeneric.MetaMcpServerID)
	require.NotNil(t, backToGeneric.McpServerID)
	require.Equal(t, mcpServerID.String(), *backToGeneric.McpServerID)
}

func TestListMcpEndpoints_FiltersByMetaBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	metaID := seedMetaMcpServer(t, ctx, ti, *authCtx.ProjectID)

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      conv.PtrEmpty(mcpServerID.String()),
		MetaMcpServerID:  nil,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-generic-listed"),
	})
	require.NoError(t, err)

	metaEndpoint, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      nil,
		MetaMcpServerID:  conv.PtrEmpty(metaID.String()),
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-meta-listed"),
	})
	require.NoError(t, err)

	result, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      nil,
		MetaMcpServerID:  conv.PtrEmpty(metaID.String()),
	})
	require.NoError(t, err)
	require.Len(t, result.McpEndpoints, 1)
	require.Equal(t, metaEndpoint.ID, result.McpEndpoints[0].ID)
}

func TestListMcpEndpoints_RejectsBothFilters(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      conv.PtrEmpty(uuid.NewString()),
		MetaMcpServerID:  conv.PtrEmpty(uuid.NewString()),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestBySlugAndCustomDomain_ResolvesMetaBackedEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	metaID := seedMetaMcpServer(t, ctx, ti, *authCtx.ProjectID)

	slug := authCtx.OrganizationSlug + "-meta-resolved"
	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      nil,
		MetaMcpServerID:  conv.PtrEmpty(metaID.String()),
		Slug:             types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)

	endpoint, server, meta, err := mcpendpoints.BySlugAndCustomDomain(ctx, ti.conn, testenv.NewLogger(t), slug)
	require.NoError(t, err)
	require.Nil(t, server, "meta-backed endpoints resolve with no generic server")
	require.NotNil(t, meta)
	require.Equal(t, metaID, meta.ID)
	require.Equal(t, slug, endpoint.Slug)
}
