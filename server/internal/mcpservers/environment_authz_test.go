package mcpservers_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func seedEnvironment(t *testing.T, ctx context.Context, ti *testInstance, slug string) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	env, err := environmentsrepo.New(ti.conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           slug,
		Slug:           slug,
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return env.ID.String()
}

func TestMcpServer_EnvironmentBindingRequiresEnvironmentRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	current := seedEnvironment(t, ctx, ti, "mcp-env-current")
	next := seedEnvironment(t, ctx, ti, "mcp-env-next")
	mcpWrite := authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource)

	create := func(ctx context.Context, environmentID string) (*types.McpServer, error) {
		return ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
			Name: "environment bound", EnvironmentID: &environmentID, RemoteMcpServerID: &remoteID,
			Visibility: types.McpServerVisibility("disabled"),
		})
	}
	update := func(ctx context.Context, serverID, environmentID string) (*types.McpServer, error) {
		return ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
			ID: serverID, EnvironmentID: &environmentID, RemoteMcpServerID: &remoteID,
			Visibility: types.McpServerVisibility("disabled"),
		})
	}

	writer := withExactAuthzGrants(t, ctx, ti.conn, mcpWrite)
	_, err := create(writer, current)
	requireOopsCode(t, err, oops.CodeForbidden)

	reader := withExactAuthzGrants(t, ctx, ti.conn, mcpWrite, authz.NewGrant(authz.ScopeEnvironmentRead, current))
	created, err := create(reader, current)
	require.NoError(t, err)

	// Resubmitting the bound environment selects nothing new.
	_, err = update(writer, created.ID, current)
	require.NoError(t, err)

	_, err = update(writer, created.ID, next)
	requireOopsCode(t, err, oops.CodeForbidden)
}
