package mcpendpoints_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

func TestCreateMcpEndpoint_PlatformDomainWithOrgPrefix(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointCreate)
	require.NoError(t, err)

	slug := authCtx.OrganizationSlug + "-example"
	result, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.ID)
	require.Equal(t, mcpServerID, result.McpServerID)
	require.Equal(t, slug, string(result.Slug))
	require.Nil(t, result.CustomDomainID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateMcpEndpoint_PlatformDomainRejectsUnprefixedSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug("some-unrelated-slug"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpEndpoint_InvalidFrontendID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      "not-a-uuid",
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-example"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateMcpEndpoint_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-example"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateMcpEndpoint_RejectsCrossTenantMcpFrontend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Frontend lives in a different project in the same org.
	otherFrontendID := seedOtherProjectMcpFrontend(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      otherFrontendID,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-leak"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpEndpoint_ConflictOnDuplicateSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	slugVal := authCtx.OrganizationSlug + "-taken"

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slugVal),
	})
	require.NoError(t, err)

	// Second create with the same (NULL custom_domain_id, slug) must conflict.
	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slugVal),
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateMcpEndpoint_ConflictOnDuplicateSlugWithCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "custom-" + uuid.NewString() + ".example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	customDomainID := domain.ID.String()

	slugVal := "taken"

	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   &customDomainID,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slugVal),
	})
	require.NoError(t, err)

	// Second create with the same (custom_domain_id, slug) must conflict.
	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   &customDomainID,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slugVal),
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateMcpEndpoint_AttachesToDefaultPlugin(t *testing.T) {
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

	// A non-disabled mcp_server, unlike seedMcpServer's "disabled" fixture,
	// since AttachToDefaultPlugin skips disabled servers.
	remoteServer := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{
		ProjectID:     *authCtx.ProjectID,
		TransportType: "streamable-http",
		Url:           "https://test.example.com/mcp/" + uuid.NewString(),
	})
	mcpServerID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                mcpServerID,
		ProjectID:         *authCtx.ProjectID,
		Name:              conv.ToPGText("Attach Test Server"),
		Slug:              conv.ToPGText("attach-test-server-" + uuid.NewString()),
		EnvironmentID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteServer.ID, Valid: true},
		ToolsetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:        "public",
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)

	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID.String(),
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-attach-test"),
	})
	require.NoError(t, err)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, mcpServerID, servers[0].McpServerID.UUID)
	require.Equal(t, "Attach Test Server", servers[0].DisplayName)
	require.Equal(t, "required", servers[0].Policy)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateMcpEndpoint_LazilyCreatesDefaultPluginWhenMissing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteServer := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{
		ProjectID:     *authCtx.ProjectID,
		TransportType: "streamable-http",
		Url:           "https://test.example.com/mcp/" + uuid.NewString(),
	})
	mcpServerID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                mcpServerID,
		ProjectID:         *authCtx.ProjectID,
		Name:              conv.ToPGText("No Default Plugin Server"),
		Slug:              conv.ToPGText("no-default-plugin-server-" + uuid.NewString()),
		EnvironmentID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteServer.ID, Valid: true},
		ToolsetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:        "public",
	})
	require.NoError(t, err)

	pluginsQueries := pluginsrepo.New(ti.conn)
	_, err = pluginsQueries.GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "fixture project (created directly via projectsrepo) has no Default plugin yet")

	beforeCreateCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginCreate)
	require.NoError(t, err)
	beforeAddCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)

	// This project predates the Default-plugin feature (no CreateProject call
	// ever ran for it) — the endpoint create must lazily provision one.
	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID.String(),
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-no-default"),
	})
	require.NoError(t, err)

	defaultPlugin, err := pluginsQueries.GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, "Default", defaultPlugin.Name)
	require.Equal(t, "default", defaultPlugin.Slug)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, mcpServerID, servers[0].McpServerID.UUID)

	afterCreateCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCreateCount+1, afterCreateCount)

	afterAddCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)
	require.Equal(t, beforeAddCount+1, afterAddCount)
}
