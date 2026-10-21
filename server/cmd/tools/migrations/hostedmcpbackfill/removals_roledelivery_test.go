package hostedmcpbackfill

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A re-keyed removal record keeps the canonical server out of the plugin on the eligibility path, as the toolset-keyed record did.
func TestRekeyedRemovalRecordStillBlocksRoleDeliveryReAdd(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	toolsetID := f.seedWrappedToolset(t, "role-delivered")
	removedFrom, control := f.seedPlugin(t), f.seedPlugin(t)

	role := "role:organization:" + uuid.NewString()
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, toolsetID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.pool).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: f.orgID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	for _, plugin := range []uuid.UUID{removedFrom, control} {
		_, err := pluginsrepo.New(f.pool).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{OrganizationID: f.orgID, PrincipalUrn: role, InstallMode: "", PluginID: plugin})
		require.NoError(t, err)
	}
	record := f.seedRemoval(t, removedFrom, key(toolsetID), uuid.NullUUID{}, "removed")

	// eligible runs the MCP-enable hook in a transaction it rolls back and returns each plugin's live members.
	eligible := func() (removed, delivered []pluginsrepo.PluginServer) {
		tx, err := f.pool.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary for the role-delivery hook under test.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(context.Background()) }()
		changed, err := roledelivery.Eligible(ctx, tx, f.orgID, f.projectID, toolsetID, nil)
		require.NoError(t, err)
		require.True(t, changed, "the control plugin receives the server")
		removed, err = pluginsrepo.New(tx).ListPluginServers(ctx, removedFrom)
		require.NoError(t, err)
		delivered, err = pluginsrepo.New(tx).ListPluginServers(ctx, control)
		require.NoError(t, err)
		return removed, delivered
	}

	removed, delivered := eligible()
	require.Empty(t, removed, "the toolset-keyed removal keeps the server out")
	require.Len(t, delivered, 1)
	require.Equal(t, key(toolsetID), delivered[0].McpServerID)

	opts, _ := rekeyOptions(true)
	report := f.run(t, opts)
	require.Equal(t, OutcomeRekeyed, dependentRowFor(t, report, record).Outcome)
	require.Equal(t, key(toolsetID), removalFor(t, f.removals(t), record).McpServerID)

	removed, delivered = eligible()
	require.Empty(t, removed, "the server-keyed removal keeps the server out")
	require.Len(t, delivered, 1)
}
