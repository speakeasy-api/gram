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
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedToolset inserts a toolsets row whose slug is slug.
func seedToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, slug string) toolsetsrepo.Toolset {
	t.Helper()

	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         organizationID,
		ProjectID:              projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return toolset
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
	unrelatedToolset := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "unrelated-"+uuid.NewString()[:8])
	available, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:             slug,
		CustomDomainID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:   authCtx.ActiveOrganizationID,
		ExcludeToolsetID: uuid.NullUUID{UUID: unrelatedToolset.ID, Valid: true},
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
	toolset := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)
	wrapperID := seedToolsetBackedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	_, err := repo.New(ti.conn).CreateMCPEndpoint(ctx, repo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: wrapperID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            slug,
	})
	require.NoError(t, err)

	// Without the exclusion the wrapper's endpoint counts.
	available, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:             slug,
		CustomDomainID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:   authCtx.ActiveOrganizationID,
		ExcludeToolsetID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.False(t, available)

	// With the exclusion, the wrapper's endpoint does not count against the toolset.
	available, err = mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:             slug,
		CustomDomainID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:   authCtx.ActiveOrganizationID,
		ExcludeToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
	})
	require.NoError(t, err)
	require.True(t, available)
}

func TestToolsetWrapperLookups_PreferCanonicalWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.OrganizationSlug+"-lookup")
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

// A toolset's own slug holds no address: only endpoints do.
func TestCreateMcpEndpoint_ToolsetSlugDoesNotHoldAddress(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := authCtx.OrganizationSlug + "-toolset-slug"
	seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)
	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      conv.PtrEmpty(mcpServerID),
		Slug:             types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)
	require.Equal(t, slug, string(created.Slug))
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

	shadowed := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID, authCtx.OrganizationSlug+"-shadowed")
	seedDisabled(shadowed.ID, shadowed.ID)
	fresh := seedToolsetBackedMcpServer(t, ctx, ti.conn, projectID, shadowed.ID)
	require.Equal(t, fresh, lookup(shadowed.ID), "a disabled canonical wrapper must not hide an enabled server")

	allOff := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID, authCtx.OrganizationSlug+"-all-off")
	allOffCanonical := seedDisabled(allOff.ID, allOff.ID)
	seedDisabled(allOff.ID, uuid.Must(uuid.NewV7()))
	require.Equal(t, allOffCanonical, lookup(allOff.ID), "among disabled servers the canonical wrapper still wins")
}
