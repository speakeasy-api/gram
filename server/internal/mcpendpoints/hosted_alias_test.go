package mcpendpoints_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// mirroredAddress is a copy of the address part of the dual-write release's MirrorOf, which pods from that release
// apply on every hosted write. It is the oracle the alias rule must agree with.
func mirroredAddress(toolset toolsetsrepo.Toolset, endpoints []mcpendpointsrepo.McpEndpoint) (pgtype.Text, uuid.NullUUID) {
	var preferred *mcpendpointsrepo.McpEndpoint
	for i := range endpoints {
		candidate := &endpoints[i]
		switch {
		case toolset.McpSlug.Valid && candidate.Slug == toolset.McpSlug.String && candidate.CustomDomainID == toolset.CustomDomainID:
			return toolset.McpSlug, toolset.CustomDomainID
		case preferred == nil:
			preferred = candidate
		case !candidate.CustomDomainID.Valid && preferred.CustomDomainID.Valid:
			preferred = candidate
		case candidate.CustomDomainID.Valid == preferred.CustomDomainID.Valid && candidate.CreatedAt.Time.Before(preferred.CreatedAt.Time):
			preferred = candidate
		}
	}
	if preferred == nil {
		return toolset.McpSlug, toolset.CustomDomainID
	}
	return pgtype.Text{String: preferred.Slug, Valid: true}, preferred.CustomDomainID
}

type aliasState struct {
	toolset   toolsetsrepo.Toolset
	endpoints []mcpendpointsrepo.McpEndpoint
}

func (f *hostedAddressFixture) aliasState(t *testing.T, ctx context.Context) aliasState {
	t.Helper()

	toolset, err := toolsetsrepo.New(f.ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	rows, err := mcpendpointsrepo.New(f.ti.conn).ListMCPEndpointsOnLiveDomainsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsOnLiveDomainsByMCPServerIDParams{
		OrganizationID: f.authCtx.ActiveOrganizationID, ProjectID: f.projectID, McpServerID: f.toolset.ID,
	})
	require.NoError(t, err)
	endpoints := make([]mcpendpointsrepo.McpEndpoint, 0, len(rows))
	for _, row := range rows {
		endpoints = append(endpoints, row.McpEndpoint)
	}
	return aliasState{toolset: toolset, endpoints: endpoints}
}

// requireAliasFollowed asserts the alias sits where the dual-write release would put it from before, and that a
// write from that release afterwards leaves the address alone.
func requireAliasFollowed(t *testing.T, before, after aliasState, wantSlug string) {
	t.Helper()

	slug, domainID := mirroredAddress(before.toolset, after.endpoints)
	require.Equal(t, slug, after.toolset.McpSlug, "the alias matches the dual-write release's choice")
	require.Equal(t, domainID, after.toolset.CustomDomainID)
	require.Equal(t, wantSlug, after.toolset.McpSlug.String)
	slug, domainID = mirroredAddress(after.toolset, after.endpoints)
	require.Equal(t, after.toolset.McpSlug, slug, "a dual-write release write afterwards keeps the address")
	require.Equal(t, after.toolset.CustomDomainID, domainID)
}

func TestHostedLegacyAliasFollowsItsEndpoint(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	_, err := toolsetsrepo.New(f.ti.conn).UpdateToolset(ctx, toolsetsrepo.UpdateToolsetParams{
		Name: f.toolset.Name, Description: f.toolset.Description, DefaultEnvironmentSlug: f.toolset.DefaultEnvironmentSlug,
		McpSlug: pgtype.Text{String: f.endpoint.Slug, Valid: true}, McpIsPublic: false, CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpEnabled: true, ToolSelectionMode: f.toolset.ToolSelectionMode, Slug: f.toolset.Slug, ProjectID: f.projectID,
	})
	require.NoError(t, err)
	org := f.authCtx.OrganizationSlug

	// Adding an address keeps the alias on the endpoint that still holds it.
	before := f.aliasState(t, ctx)
	second, err := f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(org + "-second")})
	require.NoError(t, err)
	requireAliasFollowed(t, before, f.aliasState(t, ctx), f.endpoint.Slug)

	// Another server cannot claim the held alias.
	_, err = f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		McpServerID: new(seedMcpServer(t, ctx, f.ti.conn, f.projectID).String()), Slug: types.McpEndpointSlug(f.endpoint.Slug),
	})
	requireOopsCode(t, err, oops.CodeConflict)

	// Renaming the aliased endpoint moves the alias with it.
	before = f.aliasState(t, ctx)
	renamed := org + "-renamed"
	_, err = f.ti.service.UpdateMcpEndpoint(ctx, f.payload(renamed, nil))
	require.NoError(t, err)
	requireAliasFollowed(t, before, f.aliasState(t, ctx), renamed)
	require.False(t, f.resolves(t, ctx, f.endpoint.Slug), "the old address stops serving")
	require.True(t, f.resolves(t, ctx, renamed))

	// Deleting the aliased endpoint moves the alias to the remaining preferred endpoint.
	before = f.aliasState(t, ctx)
	require.NoError(t, f.ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{ID: f.endpoint.ID.String()}))
	requireAliasFollowed(t, before, f.aliasState(t, ctx), string(second.Slug))
}

// A toolset created without an alias keeps none as its endpoints change.
func TestHostedEndpointWritesLeaveToolsetWithoutAlias(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")

	_, err := f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(f.authCtx.OrganizationSlug + "-extra")})
	require.NoError(t, err)
	require.NoError(t, f.ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{ID: f.endpoint.ID.String()}))
	require.False(t, f.aliasState(t, ctx).toolset.McpSlug.Valid)
}
