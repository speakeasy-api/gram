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
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	})
	require.NoError(t, err)
	testenv.SetLegacyAlias(t, ctx, conn, projectID, toolset.ID, mcpSlug, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	toolset, err = toolsetsrepo.New(conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: toolset.ID, ProjectID: projectID})
	require.NoError(t, err)

	return toolset
}

// bindToolsetCustomDomain moves a fixture toolset's alias at mcpSlug into a
// custom domain's address namespace.
func bindToolsetCustomDomain(t *testing.T, ctx context.Context, conn *pgxpool.Pool, toolset toolsetsrepo.Toolset, mcpSlug string, customDomainID uuid.UUID) {
	t.Helper()

	testenv.SetLegacyAlias(t, ctx, conn, toolset.ProjectID, toolset.ID, mcpSlug, uuid.NullUUID{UUID: customDomainID, Valid: true})
}

// seedToolsetBackedMcpServer wraps a toolset in an mcp_servers row, the way
// the hosted-server migration mirrors legacy toolsets.
func seedToolsetBackedMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, toolsetID uuid.UUID) uuid.UUID {
	t.Helper()

	mcpServerID, err := uuid.NewV7()
	require.NoError(t, err)
	return seedToolsetBackedMcpServerWithID(t, ctx, conn, projectID, toolsetID, mcpServerID, "private")
}

// seedCanonicalToolsetWrapper creates the toolset's canonical wrapper, whose id is the toolset id.
func seedCanonicalToolsetWrapper(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, toolsetID uuid.UUID) uuid.UUID {
	t.Helper()
	return seedToolsetBackedMcpServerWithID(t, ctx, conn, projectID, toolsetID, toolsetID, "private")
}

func seedToolsetBackedMcpServerWithID(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, toolsetID, mcpServerID uuid.UUID, visibility string) uuid.UUID {
	t.Helper()

	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  mcpServerID,
		ProjectID:           projectID,
		Name:                conv.ToPGText("hosted server"),
		Slug:                conv.ToPGText("hosted-server-" + uuid.NewString()),
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:           uuid.NullUUID{UUID: toolsetID, Valid: true},
		Visibility:          visibility,
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
	bindToolsetCustomDomain(t, ctx, ti.conn, toolset, slug, domainID)

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

func TestToolsetWrapperLookups_PreferCanonicalWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.OrganizationSlug+"-lookup")
	servers := mcpserversrepo.New(ti.conn)

	memberID := seedToolsetBackedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	got, err := servers.GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{ToolsetID: toolset.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, memberID, got.ID, "without a canonical wrapper the oldest server is returned")

	canonicalID := seedCanonicalToolsetWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	got, err = servers.GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{ToolsetID: toolset.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, canonicalID, got.ID)

	enabled, err := servers.ListEnabledMCPServersByToolsetID(ctx, mcpserversrepo.ListEnabledMCPServersByToolsetIDParams{ToolsetID: toolset.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Len(t, enabled, 1, "a canonical wrapper makes legacy attribution unambiguous")
	require.Equal(t, canonicalID, enabled[0].ID)
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

func TestCheckSlugAvailable_CustomDomainToolsetHoldsPlatformSlugEndpointDoesNot(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	domainID := seedCustomDomain(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	slug := "scoped-" + uuid.NewString()[:8]
	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)
	bindToolsetCustomDomain(t, ctx, ti.conn, toolset, slug, domainID)

	// The platform host serves a custom-domain alias too, so it holds the
	// platform slug.
	available, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               slug,
		CustomDomainID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:     authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExcludeMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.False(t, available)

	// A custom-domain endpoint does not block the same slug on the platform.
	endpointSlug := "scoped-endpoint-" + uuid.NewString()[:8]
	_, err = repo.New(ti.conn).CreateMCPEndpoint(ctx, repo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: domainID, Valid: true},
		McpServerID:    uuid.NullUUID{UUID: seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID), Valid: true},
		Slug:           endpointSlug,
	})
	require.NoError(t, err)
	available, err = mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               endpointSlug,
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

func TestGetMCPServerByToolsetID_PrefersEnabledServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	servers := mcpserversrepo.New(ti.conn)
	seedDisabled := func(toolsetID, id uuid.UUID) uuid.UUID {
		return seedToolsetBackedMcpServerWithID(t, ctx, ti.conn, projectID, toolsetID, id, "disabled")
	}
	lookup := func(toolsetID uuid.UUID) uuid.UUID {
		got, err := servers.GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{ToolsetID: toolsetID, ProjectID: projectID})
		require.NoError(t, err)
		return got.ID
	}

	shadowed := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID, authCtx.OrganizationSlug+"-shadowed")
	seedDisabled(shadowed.ID, shadowed.ID)
	fresh := seedToolsetBackedMcpServer(t, ctx, ti.conn, projectID, shadowed.ID)
	require.Equal(t, fresh, lookup(shadowed.ID), "a disabled canonical wrapper must not hide an enabled server")

	allOff := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID, authCtx.OrganizationSlug+"-all-off")
	allOffCanonical := seedDisabled(allOff.ID, allOff.ID)
	seedDisabled(allOff.ID, uuid.Must(uuid.NewV7()))
	require.Equal(t, allOffCanonical, lookup(allOff.ID), "among disabled servers the canonical wrapper still wins")
}
