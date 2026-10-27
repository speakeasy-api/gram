package platformmcp

import (
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProjectConnectionsInspectConfirmAndReplay(t *testing.T) {
	t.Parallel()
	ctx, f := seedProjectLifecycleFixture(t, t.Context(), "connections")
	input := ProjectConnectionsInput{ProjectID: f.project.ID.String()}
	before, err := f.service.ConfigureProjectConnections(ctx, f.principal, input)
	require.NoError(t, err)
	require.False(t, before.RequireMCPGateway)
	input.RequireMCPGateway = new(true)
	input.ExpectedUpdatedAt = before.UpdatedAt
	_, err = f.service.ConfigureProjectConnections(ctx, f.principal, input)
	require.Error(t, err, "confirmation is required")
	input.Confirmed = true
	after, err := f.service.ConfigureProjectConnections(ctx, f.principal, input)
	require.NoError(t, err)
	require.True(t, after.RequireMCPGateway)
	replay, err := f.service.ConfigureProjectConnections(ctx, f.principal, input)
	require.NoError(t, err)
	require.Equal(t, after, replay, "replay must not write again")
	input.RequireMCPGateway = new(false)
	_, err = f.service.ConfigureProjectConnections(ctx, f.principal, input)
	require.Error(t, err, "stale version cannot reverse a newer setting")
	input.ExpectedUpdatedAt = after.UpdatedAt
	disabled, err := f.service.ConfigureProjectConnections(ctx, f.principal, input)
	require.NoError(t, err)
	require.False(t, disabled.RequireMCPGateway)
}

func TestProjectConnectionsDeniedWithoutWrite(t *testing.T) {
	t.Parallel()
	ctx, f := seedProjectLifecycleFixture(t, t.Context(), "connections_denied")
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, f.project.ID.String())})
	_, err := f.service.ConfigureProjectConnections(ctx, f.principal, ProjectConnectionsInput{ProjectID: f.project.ID.String()})
	require.Error(t, err)
}
