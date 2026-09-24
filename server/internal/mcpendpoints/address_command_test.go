//nolint:glint // These focused transaction tests open caller-owned transactions to exercise the write seam.
package mcpendpoints_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestUpdateMcpEndpointAddressInTransaction(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	originalSlug := authCtx.OrganizationSlug + "-address-before"
	created, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		McpServerID: conv.PtrEmpty(serverID.String()),
		Slug:        types.McpEndpointSlug(originalSlug),
	})
	require.NoError(t, err)

	beforeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointUpdate)
	require.NoError(t, err)
	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	updated, reconcileDomains, err := ti.service.UpdateMcpEndpointAddressInTransaction(ctx, tx, mcpendpoints.UpdateMcpEndpointAddressInput{
		AuthContext:    authCtx,
		EndpointID:     uuid.MustParse(created.ID),
		CustomDomainID: uuid.NullUUID{},
		Slug:           authCtx.OrganizationSlug + "-address-after",
	})
	require.NoError(t, err)
	require.Empty(t, reconcileDomains)
	require.Equal(t, created.ID, updated.ID)
	require.Equal(t, serverID.String(), *updated.McpServerID)
	require.Equal(t, authCtx.OrganizationSlug+"-address-after", string(updated.Slug))
	require.NoError(t, tx.Commit(ctx))

	afterAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount+1, afterAuditCount)
	stored, err := ti.service.GetMcpEndpoint(ctx, &gen.GetMcpEndpointPayload{ID: &created.ID})
	require.NoError(t, err)
	require.Equal(t, originalSlug, string(created.Slug))
	require.Equal(t, authCtx.OrganizationSlug+"-address-after", string(stored.Slug))
}

func TestUpdateMcpEndpointAddressInTransactionRejectsSlugCollision(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	first, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: conv.PtrEmpty(serverID.String()), Slug: types.McpEndpointSlug(authCtx.OrganizationSlug + "-first")})
	require.NoError(t, err)
	second, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: conv.PtrEmpty(serverID.String()), Slug: types.McpEndpointSlug(authCtx.OrganizationSlug + "-second")})
	require.NoError(t, err)

	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, _, err = ti.service.UpdateMcpEndpointAddressInTransaction(ctx, tx, mcpendpoints.UpdateMcpEndpointAddressInput{
		AuthContext: authCtx, EndpointID: uuid.MustParse(second.ID), Slug: string(first.Slug),
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestUpdateMcpEndpointAddressInTransactionRejectsForeignDomain(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	created, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: conv.PtrEmpty(serverID.String()), Slug: types.McpEndpointSlug(authCtx.OrganizationSlug + "-owned-domain")})
	require.NoError(t, err)
	foreignDomain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: "org_" + uuid.NewString(), Domain: "foreign-" + uuid.NewString() + ".example.com", IpAllowlist: []string{},
	})
	require.NoError(t, err)

	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, _, err = ti.service.UpdateMcpEndpointAddressInTransaction(ctx, tx, mcpendpoints.UpdateMcpEndpointAddressInput{
		AuthContext: authCtx, EndpointID: uuid.MustParse(created.ID), CustomDomainID: uuid.NullUUID{UUID: foreignDomain.ID, Valid: true}, Slug: "foreign-target",
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpEndpointAddressInTransactionPreservesRoot(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID, Domain: "address-root-" + uuid.NewString() + ".example.com", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	serverID := seedMcpServerWithVisibility(t, ctx, ti.conn, *authCtx.ProjectID, "public")
	endpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: *authCtx.ProjectID, CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true}, McpServerID: uuid.NullUUID{UUID: serverID, Valid: true}, Slug: "before",
	})
	require.NoError(t, err)
	require.NoError(t, customdomainsrepo.New(ti.conn).SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{McpEndpointID: endpoint.ID, CustomDomainID: domain.ID}))

	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	updated, reconcileDomains, err := ti.service.UpdateMcpEndpointAddressInTransaction(ctx, tx, mcpendpoints.UpdateMcpEndpointAddressInput{
		AuthContext: authCtx, EndpointID: endpoint.ID, CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true}, Slug: "after",
	})
	require.NoError(t, err)
	require.Empty(t, reconcileDomains)
	require.True(t, updated.IsDomainRoot)
	require.NoError(t, tx.Commit(ctx))
	route, err := customdomainsrepo.New(ti.conn).GetCustomDomainRouteConfig(ctx, domain.ID)
	require.NoError(t, err)
	require.Equal(t, "after", route.RootSlug)
}

func TestCreateMcpEndpointInTransactionDoesNotAttachDefaultPlugin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := seedMcpServerWithVisibility(t, ctx, ti.conn, *authCtx.ProjectID, "public")
	plugin, err := pluginsrepo.New(ti.conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	created, err := ti.service.CreateMcpEndpointInTransaction(ctx, tx, mcpendpoints.CreateMcpEndpointInTransactionInput{
		AuthContext: authCtx, McpServerID: uuid.NullUUID{UUID: serverID, Valid: true},
		Slug: authCtx.OrganizationSlug + "-transaction-created",
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.NoError(t, tx.Commit(ctx))
	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	members, err := pluginsrepo.New(ti.conn).ListPluginServers(ctx, plugin.ID)
	require.NoError(t, err)
	for _, member := range members {
		require.NotEqual(t, serverID, member.McpServerID.UUID)
	}
}

func TestUpdateMcpEndpointAddressConcurrentWritersSerializeSlugClaims(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	create := func(slug string) uuid.UUID {
		endpoint, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: conv.PtrEmpty(serverID.String()), Slug: types.McpEndpointSlug(authCtx.OrganizationSlug + "-" + slug)})
		require.NoError(t, err)
		return uuid.MustParse(endpoint.ID)
	}
	firstID, secondID := create("concurrent-a"), create("concurrent-b")
	claimedSlug := authCtx.OrganizationSlug + "-concurrent-target"
	results := make(chan error, 2)
	for _, endpointID := range []uuid.UUID{firstID, secondID} {
		go func() {
			tx, err := ti.conn.Begin(ctx)
			if err != nil {
				results <- err
				return
			}
			_, _, err = ti.service.UpdateMcpEndpointAddressInTransaction(ctx, tx, mcpendpoints.UpdateMcpEndpointAddressInput{AuthContext: authCtx, EndpointID: endpointID, Slug: claimedSlug})
			if err != nil {
				_ = tx.Rollback(ctx)
				results <- err
				return
			}
			results <- tx.Commit(ctx)
		}()
	}
	firstErr, secondErr := <-results, <-results
	require.NotEqual(t, firstErr == nil, secondErr == nil, "exactly one concurrent writer should claim the slug")
	conflict := firstErr
	if conflict == nil {
		conflict = secondErr
	}
	requireOopsCode(t, conflict, oops.CodeConflict)
}
