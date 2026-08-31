package mcpendpoints_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedHostedToolset inserts a toolsets row carrying a live mcp_slug, the
// legacy representation of a hosted MCP server's address.
func seedHostedToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, mcpSlug string) toolsetsrepo.Toolset {
	t.Helper()

	slug := "fixture-" + uuid.NewString()[:8]
	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         organizationID,
		ProjectID:              projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(mcpSlug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	return toolset
}

// bindToolsetCustomDomain moves a fixture toolset's mcp_slug into a custom
// domain's address namespace.
func bindToolsetCustomDomain(t *testing.T, ctx context.Context, conn *pgxpool.Pool, toolset toolsetsrepo.Toolset, customDomainID uuid.UUID) {
	t.Helper()

	err := toolsetsrepo.New(conn).SetToolsetCustomDomain(ctx, toolsetsrepo.SetToolsetCustomDomainParams{
		CustomDomainID: uuid.NullUUID{UUID: customDomainID, Valid: true},
		Slug:           toolset.Slug,
		ProjectID:      toolset.ProjectID,
	})
	require.NoError(t, err)
}

// seedToolsetBackedMcpServer wraps a toolset in an mcp_servers row, the way
// the hosted-server migration mirrors legacy toolsets.
func seedToolsetBackedMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, toolsetID uuid.UUID) uuid.UUID {
	t.Helper()

	mcpServerID, err := uuid.NewV7()
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  mcpServerID,
		ProjectID:           projectID,
		Name:                conv.ToPGText("hosted server"),
		Slug:                conv.ToPGText("hosted-server-" + uuid.NewString()),
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:           uuid.NullUUID{UUID: toolsetID, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)

	return server.ID
}

func seedCustomDomain(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	domain, err := customdomainsrepo.New(conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: organizationID,
		Domain:         "custom-" + uuid.NewString() + ".example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)

	return domain.ID
}

func TestCheckSlugAvailable_ToolsetPlatformSlugBlocksEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "unified-taken-" + uuid.NewString()[:8]
	seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)

	available, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               slug,
		CustomDomainID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:     authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExcludeMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.False(t, available)

	// The mcpEndpoints RPC sees the same unified namespace.
	rpcAvailable, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             types.McpEndpointSlug(slug),
		CustomDomainID:   nil,
	})
	require.NoError(t, err)
	require.False(t, rpcAvailable)
}

func TestCheckSlugAvailable_ToolsetCustomDomainSlugBlocksEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	domainID := seedCustomDomain(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	slug := "unified-domain-" + uuid.NewString()[:8]
	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)
	bindToolsetCustomDomain(t, ctx, ti.conn, toolset, domainID)

	available, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               slug,
		CustomDomainID:     uuid.NullUUID{UUID: domainID, Valid: true},
		OrganizationID:     authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExcludeMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.False(t, available)
}

func TestCheckSlugAvailable_EndpointSlugBlocksToolsetValidation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := authCtx.OrganizationSlug + "-endpoint-first"
	_, err := repo.New(ti.conn).CreateMCPEndpoint(ctx, repo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: mcpServerID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            slug,
	})
	require.NoError(t, err)

	// A toolset validating this slug for itself (owner exclusion set to an
	// unrelated toolset id) finds it taken by the endpoint.
	unrelatedToolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "unrelated-"+uuid.NewString()[:8])
	available, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               slug,
		CustomDomainID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:     authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   uuid.NullUUID{UUID: unrelatedToolset.ID, Valid: true},
		ExcludeMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.False(t, available)
}

func TestCheckSlugAvailable_OwnerExclusionToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := authCtx.OrganizationSlug + "-own-address"
	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)
	wrapperID := seedToolsetBackedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	_, err := repo.New(ti.conn).CreateMCPEndpoint(ctx, repo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: wrapperID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            slug,
	})
	require.NoError(t, err)

	// Without the exclusion the toolset's own row and its wrapper's endpoint
	// both count.
	available, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               slug,
		CustomDomainID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:     authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExcludeMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.False(t, available)

	// With the exclusion, neither the toolset's own row nor its wrapper's
	// endpoint counts against it.
	available, err = mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               slug,
		CustomDomainID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:     authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ExcludeMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.True(t, available)
}

func TestCheckSlugAvailable_OwnerExclusionMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := authCtx.OrganizationSlug + "-backing-toolset"
	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)
	wrapperID := seedToolsetBackedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)

	// Without the exclusion the backing toolset's slug counts.
	available, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               slug,
		CustomDomainID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:     authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExcludeMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.False(t, available)

	// The toolset backing the server being validated does not count against
	// that server.
	available, err = mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               slug,
		CustomDomainID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:     authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExcludeMcpServerID: uuid.NullUUID{UUID: wrapperID, Valid: true},
	})
	require.NoError(t, err)
	require.True(t, available)
}

func TestCheckSlugAvailable_CustomDomainDoesNotCollideWithPlatform(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	domainID := seedCustomDomain(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	slug := "scoped-" + uuid.NewString()[:8]
	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)
	bindToolsetCustomDomain(t, ctx, ti.conn, toolset, domainID)

	// The custom-domain address does not block the same slug on the platform
	// scope.
	available, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               slug,
		CustomDomainID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:     authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExcludeMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.True(t, available)

	// And a platform toolset address does not block the same slug under a
	// custom domain.
	platformSlug := "scoped-platform-" + uuid.NewString()[:8]
	seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, platformSlug)
	available, err = mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               platformSlug,
		CustomDomainID:     uuid.NullUUID{UUID: domainID, Valid: true},
		OrganizationID:     authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExcludeMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.True(t, available)
}

func TestCreateMcpEndpoint_ConflictsWithToolsetMcpSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := authCtx.OrganizationSlug + "-legacy-address"
	seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)
	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      conv.PtrEmpty(mcpServerID),
		Slug:             types.McpEndpointSlug(slug),
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestUpdateMcpEndpoint_ConflictsWithToolsetMcpSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	takenSlug := authCtx.OrganizationSlug + "-legacy-taken"
	seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, takenSlug)
	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      conv.PtrEmpty(mcpServerID),
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-free-slug"),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		CustomDomainID:   nil,
		McpServerID:      conv.PtrEmpty(mcpServerID),
		MetaMcpServerID:  nil,
		Slug:             types.McpEndpointSlug(takenSlug),
	})
	requireOopsCode(t, err, oops.CodeConflict)
}
