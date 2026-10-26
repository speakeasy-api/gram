package mcpendpoints_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostingstate"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// mirroredAddress is a copy of the address part of the dual-write release's MirrorOf, which pods from that release
// apply on every hosted write. It is the oracle the alias rule must agree with.
func mirroredAddress(alias hostingstate.Address, endpoints []mcpendpointsrepo.McpEndpoint) (pgtype.Text, uuid.NullUUID) {
	var preferred *mcpendpointsrepo.McpEndpoint
	for i := range endpoints {
		candidate := &endpoints[i]
		switch {
		case alias.Slug.Valid && candidate.Slug == alias.Slug.String && candidate.CustomDomainID == alias.CustomDomainID:
			return alias.Slug, alias.CustomDomainID
		case preferred == nil:
			preferred = candidate
		case !candidate.CustomDomainID.Valid && preferred.CustomDomainID.Valid:
			preferred = candidate
		case candidate.CustomDomainID.Valid == preferred.CustomDomainID.Valid && candidate.CreatedAt.Time.Before(preferred.CreatedAt.Time):
			preferred = candidate
		}
	}
	if preferred == nil {
		return alias.Slug, alias.CustomDomainID
	}
	return pgtype.Text{String: preferred.Slug, Valid: true}, preferred.CustomDomainID
}

type aliasState struct {
	alias     hostingstate.Address
	endpoints []mcpendpointsrepo.McpEndpoint
}

func (f *hostedAddressFixture) aliasState(t *testing.T, ctx context.Context) aliasState {
	t.Helper()

	alias, err := legacyalias.Get(ctx, f.ti.conn, f.projectID, f.toolset.ID)
	require.NoError(t, err)
	rows, err := mcpendpointsrepo.New(f.ti.conn).ListMCPEndpointsOnLiveDomainsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsOnLiveDomainsByMCPServerIDParams{
		OrganizationID: f.authCtx.ActiveOrganizationID, ProjectID: f.projectID, McpServerID: f.toolset.ID,
	})
	require.NoError(t, err)
	endpoints := make([]mcpendpointsrepo.McpEndpoint, 0, len(rows))
	for _, row := range rows {
		endpoints = append(endpoints, row.McpEndpoint)
	}
	return aliasState{alias: alias, endpoints: endpoints}
}

// requireAliasFollowed asserts the alias sits where the dual-write release would put it from before, and that a
// write from that release afterwards leaves the address alone.
func requireAliasFollowed(t *testing.T, before, after aliasState, wantSlug string) {
	t.Helper()

	slug, domainID := mirroredAddress(before.alias, after.endpoints)
	require.Equal(t, slug, after.alias.Slug, "the alias matches the dual-write release's choice")
	require.Equal(t, domainID, after.alias.CustomDomainID)
	require.Equal(t, wantSlug, after.alias.Slug.String)
	slug, domainID = mirroredAddress(after.alias, after.endpoints)
	require.Equal(t, after.alias.Slug, slug, "a dual-write release write afterwards keeps the address")
	require.Equal(t, after.alias.CustomDomainID, domainID)
}

func TestHostedLegacyAliasFollowsItsEndpoint(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	testenv.SetLegacyAlias(t, ctx, f.ti.conn, f.projectID, f.toolset.ID, f.endpoint.Slug, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
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
	require.False(t, f.aliasState(t, ctx).alias.Slug.Valid)
}
