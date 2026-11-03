package mcpendpoints_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	legacyaliasrepo "github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedCustomDomainAliasInOtherProject gives another project in the caller's organization a hosted toolset whose
// legacy alias sits on a custom domain, which the platform host also serves.
func seedCustomDomainAliasInOtherProject(t *testing.T, ctx context.Context, ti *testInstance) (uuid.UUID, toolsetsrepo.Toolset, string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	suffix := uuid.NewString()[:8]
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "victim-" + suffix, Slug: "victim-" + suffix, OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	slug := authCtx.OrganizationSlug + "-legacy-" + suffix
	// The alias's domain row may belong to any organization; only one live domain per organization is allowed.
	domainID := seedCustomDomain(t, ctx, ti.conn, "org-alias-"+suffix)
	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, project.ID, slug)
	bindToolsetCustomDomain(t, ctx, ti.conn, toolset, slug, domainID)
	seedCanonicalToolsetWrapper(t, ctx, ti.conn, project.ID, toolset.ID)
	return project.ID, toolset, slug
}

func platformSlugAvailable(t *testing.T, ctx context.Context, ti *testInstance, slug string, excludeToolsetID, excludeServerID uuid.NullUUID) bool {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	available, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:               slug,
		CustomDomainID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:     authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   excludeToolsetID,
		ExcludeMcpServerID: excludeServerID,
	})
	require.NoError(t, err)
	return available
}

func TestCustomDomainAliasHoldsPlatformSlugAgainstOtherProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, _, slug := seedCustomDomainAliasInOtherProject(t, ctx, ti)

	rpcAvailable, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
		Slug: types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)
	require.False(t, rpcAvailable, "the dashboard must show the platform slug as taken")

	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		McpServerID: conv.PtrEmpty(serverID.String()),
		Slug:        types.McpEndpointSlug(slug),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.Contains(t, err.Error(), "already")

	created, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		McpServerID: conv.PtrEmpty(serverID.String()),
		Slug:        types.McpEndpointSlug(authCtx.OrganizationSlug + "-own-" + uuid.NewString()[:8]),
	})
	require.NoError(t, err)
	_, err = ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		ID:          created.ID,
		McpServerID: conv.PtrEmpty(serverID.String()),
		Slug:        types.McpEndpointSlug(slug),
	})
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.Contains(t, err.Error(), "already")
}

func TestCustomDomainAliasPlatformSlugOwnerExceptions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, toolset, slug := seedCustomDomainAliasInOtherProject(t, ctx, ti)
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}

	require.False(t, platformSlugAvailable(t, ctx, ti, slug, none, none))
	require.True(t, platformSlugAvailable(t, ctx, ti, slug, none, uuid.NullUUID{UUID: toolset.ID, Valid: true}), "the canonical server may claim its own alias")
	require.True(t, platformSlugAvailable(t, ctx, ti, slug, uuid.NullUUID{UUID: toolset.ID, Valid: true}, none), "the toolset may claim its own alias")

	nonCanonical := seedToolsetBackedMcpServer(t, ctx, ti.conn, projectID, toolset.ID)
	require.False(t, platformSlugAvailable(t, ctx, ti, slug, none, uuid.NullUUID{UUID: nonCanonical, Valid: true}), "only the canonical server is the owner")
}

func TestCustomDomainAliasInDeletedProjectFreesPlatformSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, _, slug := seedCustomDomainAliasInOtherProject(t, ctx, ti)
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	require.False(t, platformSlugAvailable(t, ctx, ti, slug, none, none))
	platform := legacyalias.Alias{Slug: pgtype.Text{String: slug, Valid: true}, CustomDomainID: none}
	taken, err := legacyalias.TakenAddresses(ctx, ti.conn, []legacyalias.Alias{platform})
	require.NoError(t, err)
	require.True(t, taken[platform], "the platform address of a custom-domain alias is taken")

	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, projectID)
	require.NoError(t, err)
	require.True(t, platformSlugAvailable(t, ctx, ti, slug, none, none))
	taken, err = legacyalias.TakenAddresses(ctx, ti.conn, []legacyalias.Alias{platform})
	require.NoError(t, err)
	require.False(t, taken[platform])
}

// A custom-domain alias in a deleted project no longer holds its own custom-domain address.
func TestCustomDomainAliasInDeletedProjectFreesItsAddress(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, toolset, _ := seedCustomDomainAliasInOtherProject(t, ctx, ti)
	alias, err := legacyalias.Get(ctx, ti.conn, projectID, toolset.ID)
	require.NoError(t, err)
	require.True(t, alias.CustomDomainID.Valid)
	check := legacyalias.TakenCheck{Slug: alias.Slug.String, CustomDomainID: alias.CustomDomainID, ExcludeToolsetID: uuid.NullUUID{}, ExcludeMcpServerID: uuid.NullUUID{}}

	held, err := legacyalias.Taken(ctx, ti.conn, check)
	require.NoError(t, err)
	require.True(t, held)
	taken, err := legacyalias.TakenAddresses(ctx, ti.conn, []legacyalias.Alias{alias})
	require.NoError(t, err)
	require.True(t, taken[alias])

	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, projectID)
	require.NoError(t, err)
	held, err = legacyalias.Taken(ctx, ti.conn, check)
	require.NoError(t, err)
	require.False(t, held)
	taken, err = legacyalias.TakenAddresses(ctx, ti.conn, []legacyalias.Alias{alias})
	require.NoError(t, err)
	require.False(t, taken[alias])
}

func TestPlatformAliasOwnerIsNotBlockedByCustomDomainAlias(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, _, slug := seedCustomDomainAliasInOtherProject(t, ctx, ti)

	// A platform alias with the slug wins the loose lookup, so the custom-domain alias is not served there.
	platform := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	require.True(t, platformSlugAvailable(t, ctx, ti, slug, uuid.NullUUID{UUID: platform.ID, Valid: true}, none))
	require.False(t, platformSlugAvailable(t, ctx, ti, slug, none, none))
}

func TestHostedAddressCannotTakeCustomDomainAliasOfOtherProject(t *testing.T) {
	t.Parallel()

	ctx, f := newHostedAddressFixture(t, "pro")
	_, _, slug := seedCustomDomainAliasInOtherProject(t, ctx, f.ti)

	_, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload(slug, nil))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.Contains(t, err.Error(), "already")

	_, err = f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(slug)})
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.Contains(t, err.Error(), "already")
	require.Len(t, f.endpoints(t, ctx), 1)
}

func TestHostedAliasCannotMoveOntoAnotherCustomDomainAliasSlug(t *testing.T) {
	t.Parallel()

	ctx, f := newHostedAddressFixture(t, "pro")
	testenv.SetLegacyAlias(t, ctx, f.ti.conn, f.projectID, f.toolset.ID, f.endpoint.Slug, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	_, _, slug := seedCustomDomainAliasInOtherProject(t, ctx, f.ti)
	domain := f.activeDomain(t, ctx, "own-"+uuid.NewString()[:8]+".example.com")

	_, err := f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(slug), CustomDomainID: new(domain.ID.String()),
	})
	require.NoError(t, err, "the custom-domain endpoint itself does not touch the platform")

	// Removing the platform endpoint would move the alias onto that slug, where the platform host would serve two.
	err = f.ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{ID: f.endpoint.ID.String()})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.Contains(t, err.Error(), "already taken")
	stored, err := legacyalias.Get(ctx, f.ti.conn, f.projectID, f.toolset.ID)
	require.NoError(t, err)
	require.Equal(t, f.endpoint.Slug, stored.Slug.String)
	require.False(t, stored.CustomDomainID.Valid)
}

func TestCheckSlugAvailabilityLetsServerClaimItsOwnAlias(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	suffix := uuid.NewString()[:8]
	slug := authCtx.OrganizationSlug + "-own-alias-" + suffix
	domainID := seedCustomDomain(t, ctx, ti.conn, "own-alias-"+suffix)
	toolset := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)
	bindToolsetCustomDomain(t, ctx, ti.conn, toolset, slug, domainID)
	seedCanonicalToolsetWrapper(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)

	check := func(serverID *string) bool {
		available, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
			Slug: types.McpEndpointSlug(slug), McpServerID: serverID,
		})
		require.NoError(t, err)
		return available
	}

	require.False(t, check(nil))
	require.True(t, check(conv.PtrEmpty(toolset.ID.String())), "the canonical server may claim its own alias")
	nonCanonical := seedToolsetBackedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
	require.False(t, check(conv.PtrEmpty(nonCanonical.String())))

	_, other, otherSlug := seedCustomDomainAliasInOtherProject(t, ctx, ti)
	_, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
		Slug: types.McpEndpointSlug(otherSlug), McpServerID: conv.PtrEmpty(other.ID.String()),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr, "a server outside the project cannot discount its alias")
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestMovingPlatformAliasDoesNotHideOtherCustomDomainAlias(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, other, slug := seedCustomDomainAliasInOtherProject(t, ctx, ti)
	mover := seedHostedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)

	served, err := legacyaliasrepo.New(ti.conn).OtherCustomDomainAliasServedOnPlatform(ctx, legacyaliasrepo.OtherCustomDomainAliasServedOnPlatformParams{
		Slug: conv.ToPGText(slug), ToolsetID: mover.ID,
	})
	require.NoError(t, err)
	require.True(t, served, "the mover's own platform alias leaves with it, so the other alias would contend")

	served, err = legacyaliasrepo.New(ti.conn).OtherCustomDomainAliasServedOnPlatform(ctx, legacyaliasrepo.OtherCustomDomainAliasServedOnPlatformParams{
		Slug: conv.ToPGText(slug), ToolsetID: other.ID,
	})
	require.NoError(t, err)
	require.False(t, served)
}
