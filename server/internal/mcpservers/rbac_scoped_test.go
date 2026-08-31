package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type remoteServerFixture struct {
	server *types.McpServer
	// remoteMcpServerID is the backend id needed to round-trip updates, which
	// replace the full backend reference set.
	remoteMcpServerID string
}

func createRemoteServerFixture(t *testing.T, ctx context.Context, ti *testInstance, name string) remoteServerFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	backendID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  name,
		EnvironmentID:         nil,
		RemoteMcpServerID:     &backendID,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	return remoteServerFixture{server: created, remoteMcpServerID: backendID}
}

func createToolsetBackedServerFixture(t *testing.T, ctx context.Context, ti *testInstance, name string) (*types.McpServer, uuid.UUID) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   name + " toolset",
		Slug:                   "rbac-scoped-" + uuid.NewString(),
		Description:            conv.ToPGText("scoped rbac fixture"),
		DefaultEnvironmentSlug: conv.ToPGTextEmpty(""),
		McpSlug:                conv.ToPGTextEmpty(""),
		McpEnabled:             false,
	})
	require.NoError(t, err)

	toolsetID := toolset.ID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  name,
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   nil,
		ToolsetID:             &toolsetID,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	require.NoError(t, err)

	return created, toolset.ID
}

func getMcpServerByID(ctx context.Context, ti *testInstance, id string) (*types.McpServer, error) {
	server, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		ID:               &id,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	return server, err //nolint:wrapcheck // returned for oops-code assertions on the chain
}

func TestGetMcpServer_RBAC_ServerScopedGrantAllowsOnlyThatServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	a := createRemoteServerFixture(t, ctx, ti, "scoped get a")
	b := createRemoteServerFixture(t, ctx, ti, "scoped get b")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, a.server.ID))

	fetched, err := getMcpServerByID(scoped, ti, a.server.ID)
	require.NoError(t, err)
	require.Equal(t, a.server.ID, fetched.ID)

	_, err = getMcpServerByID(scoped, ti, b.server.ID)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetMcpServer_RBAC_ToolsetScopedGrantAllows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, toolsetID := createToolsetBackedServerFixture(t, ctx, ti, "scoped toolset get")

	// The grant resource id for a toolset-backed server is the toolset id,
	// matching the ids checked at serve time and on the toolsets surface.
	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, toolsetID.String()))

	fetched, err := getMcpServerByID(scoped, ti, server.ID)
	require.NoError(t, err)
	require.Equal(t, server.ID, fetched.ID)
}

func TestListMcpServers_RBAC_FiltersToGrantedServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	a := createRemoteServerFixture(t, ctx, ti, "scoped list a")
	_ = createRemoteServerFixture(t, ctx, ti, "scoped list b")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, a.server.ID))

	result, err := ti.service.ListMcpServers(scoped, &gen.ListMcpServersPayload{
		RemoteMcpServerID:    nil,
		TunneledMcpServerID:  nil,
		ToolsetID:            nil,
		UnproxiedMcpServerID: nil,
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpServers, 1)
	require.Equal(t, a.server.ID, result.McpServers[0].ID)
}

func TestListMcpServers_RBAC_NoGrantsReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_ = createRemoteServerFixture(t, ctx, ti, "scoped list empty")

	denied := withExactAuthzGrants(t, ctx, ti.conn)

	result, err := ti.service.ListMcpServers(denied, &gen.ListMcpServersPayload{
		RemoteMcpServerID:    nil,
		TunneledMcpServerID:  nil,
		ToolsetID:            nil,
		UnproxiedMcpServerID: nil,
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.McpServers)
}

func TestUpdateMcpServer_RBAC_ServerScopedWriteAllows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	a := createRemoteServerFixture(t, ctx, ti, "scoped update a")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, a.server.ID))

	name := "scoped update a renamed"
	updated, err := ti.service.UpdateMcpServer(scoped, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    a.server.ID,
		Name:                  &name,
		EnvironmentID:         nil,
		RemoteMcpServerID:     &a.remoteMcpServerID,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.Equal(t, name, conv.PtrValOr(updated.Name, ""))
}

func TestUpdateMcpServer_RBAC_OtherServerGrantDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	a := createRemoteServerFixture(t, ctx, ti, "scoped update denied a")
	b := createRemoteServerFixture(t, ctx, ti, "scoped update denied b")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, b.server.ID))

	name := "should not apply"
	_, err := ti.service.UpdateMcpServer(scoped, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    a.server.ID,
		Name:                  &name,
		EnvironmentID:         nil,
		RemoteMcpServerID:     &a.remoteMcpServerID,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUpdateMcpServer_RBAC_ReadGrantDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	a := createRemoteServerFixture(t, ctx, ti, "scoped update read-only")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, a.server.ID))

	name := "should not apply"
	_, err := ti.service.UpdateMcpServer(scoped, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    a.server.ID,
		Name:                  &name,
		EnvironmentID:         nil,
		RemoteMcpServerID:     &a.remoteMcpServerID,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestDeleteMcpServer_RBAC_ServerScopedWriteAllows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	a := createRemoteServerFixture(t, ctx, ti, "scoped delete a")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, a.server.ID))

	err := ti.service.DeleteMcpServer(scoped, &gen.DeleteMcpServerPayload{
		ID:               a.server.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	_, err = getMcpServerByID(ctx, ti, a.server.ID)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteMcpServer_RBAC_OtherServerGrantDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	a := createRemoteServerFixture(t, ctx, ti, "scoped delete denied a")
	b := createRemoteServerFixture(t, ctx, ti, "scoped delete denied b")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, b.server.ID))

	err := ti.service.DeleteMcpServer(scoped, &gen.DeleteMcpServerPayload{
		ID:               a.server.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	fetched, err := getMcpServerByID(ctx, ti, a.server.ID)
	require.NoError(t, err)
	require.Equal(t, a.server.ID, fetched.ID)
}
