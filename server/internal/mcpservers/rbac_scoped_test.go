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

func TestGetMcpServer_RBAC_RowIdGrantDeniedOnToolsetBackedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, _ := createToolsetBackedServerFixture(t, ctx, ti, "scoped row-id denied")

	// A toolset-backed server's grant id is its toolset id, so a grant naming
	// the raw row id must not authorize it — the inverse of the toolset branch.
	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, server.ID))

	_, err := getMcpServerByID(scoped, ti, server.ID)
	requireOopsCode(t, err, oops.CodeForbidden)
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

// The following tests exercise the toolset-id grant branch of grantResourceID
// across every management endpoint (only Get is covered above), and pin the
// UpdateMcpServer invariant that authorization keys on the server's existing
// backing rather than the backing the payload switches it to.

func TestListMcpServers_RBAC_ToolsetScopedGrantReturnsServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, toolsetID := createToolsetBackedServerFixture(t, ctx, ti, "scoped list toolset")
	_ = createRemoteServerFixture(t, ctx, ti, "scoped list toolset other")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, toolsetID.String()))

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
	require.Equal(t, server.ID, result.McpServers[0].ID)
}

func TestListToolFilters_RBAC_ToolsetScopedGrantAllows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, toolsetID := createToolsetBackedServerFixture(t, ctx, ti, "scoped tool filters")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, toolsetID.String()))

	_, err := ti.service.ListToolFilters(scoped, &gen.ListToolFiltersPayload{
		ID:               &server.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestListToolFilters_RBAC_OtherServerGrantDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, _ := createToolsetBackedServerFixture(t, ctx, ti, "scoped tool filters denied")
	other := createRemoteServerFixture(t, ctx, ti, "scoped tool filters denied other")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, other.server.ID))

	_, err := ti.service.ListToolFilters(scoped, &gen.ListToolFiltersPayload{
		ID:               &server.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestDeleteMcpServer_RBAC_ToolsetScopedGrantAllows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, toolsetID := createToolsetBackedServerFixture(t, ctx, ti, "scoped delete toolset")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, toolsetID.String()))

	err := ti.service.DeleteMcpServer(scoped, &gen.DeleteMcpServerPayload{
		ID:               server.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	_, err = getMcpServerByID(ctx, ti, server.ID)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateMcpServer_RBAC_ToolsetScopedGrantAllows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, toolsetID := createToolsetBackedServerFixture(t, ctx, ti, "scoped update toolset")

	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, toolsetID.String()))

	name := "scoped update toolset renamed"
	toolsetIDStr := toolsetID.String()
	updated, err := ti.service.UpdateMcpServer(scoped, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    server.ID,
		Name:                  &name,
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   nil,
		ToolsetID:             &toolsetIDStr,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.Equal(t, name, conv.PtrValOr(updated.Name, ""))
}

// A grant on the server's existing backing authorizes an update that switches
// the backing to a target the caller has no grant on — authorization keys on
// the row as it exists, not the payload's new backing.
func TestUpdateMcpServer_RBAC_AuthorizesOnExistingBackingAllowsSwitch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, toolsetID := createToolsetBackedServerFixture(t, ctx, ti, "scoped update switch allow")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	newBackend := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	// Grant is on the existing toolset backing only, not the remote target.
	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, toolsetID.String()))

	updated, err := ti.service.UpdateMcpServer(scoped, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    server.ID,
		Name:                  nil,
		EnvironmentID:         nil,
		RemoteMcpServerID:     &newBackend,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.RemoteMcpServerID)
	require.Equal(t, newBackend, *updated.RemoteMcpServerID)
}

// A grant on the payload's target backing does NOT authorize an update of a
// server the caller has no grant on — proving the check keys on the existing
// backing, so a grant on a new toolset can't be used to hijack a server.
func TestUpdateMcpServer_RBAC_TargetBackingGrantDoesNotAuthorize(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createRemoteServerFixture(t, ctx, ti, "scoped update switch deny")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	targetToolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "scoped update switch deny target toolset",
		Slug:                   "rbac-scoped-" + uuid.NewString(),
		Description:            conv.ToPGText("scoped rbac fixture"),
		DefaultEnvironmentSlug: conv.ToPGTextEmpty(""),
		McpSlug:                conv.ToPGTextEmpty(""),
		McpEnabled:             false,
	})
	require.NoError(t, err)

	// Grant is on the toolset the payload would switch TO, not the server's
	// existing remote backing.
	scoped := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, targetToolset.ID.String()))

	targetToolsetID := targetToolset.ID.String()
	_, err = ti.service.UpdateMcpServer(scoped, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    server.server.ID,
		Name:                  nil,
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   nil,
		ToolsetID:             &targetToolsetID,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
