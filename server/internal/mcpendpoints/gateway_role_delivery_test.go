package mcpendpoints_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func TestGatewayEndpointDeliversExistingRoleGrant(t *testing.T) {
	t.Parallel()
	for _, transactional := range []bool{false, true} {
		name := "api"
		if transactional {
			name = "shared transaction"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			ac, _ := contextvalues.GetAuthContext(ctx)
			features := &feature.InMemory{}
			features.SetFlag(feature.FlagGatewayPluginMembership, ac.ActiveOrganizationID, true)
			ti.service.WithDistributionAdmission(admission.NewGuard().WithGatewayPluginMembershipFeatures(features))
			issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
				ProjectID: *ac.ProjectID, OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), Slug: "gateway-delivery",
				AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
			})
			require.NoError(t, err)
			gateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
				OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Role gateway", Visibility: "private",
				UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
			})
			require.NoError(t, err)
			gatewayID := gateway.ID
			role, err := accessrepo.New(ti.conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
				OrganizationID: ac.ActiveOrganizationID, WorkosSlug: "gateway-delivery", WorkosName: "Gateway delivery",
				WorkosCreatedAt: conv.ToPGTimestamptz(time.Now()), WorkosUpdatedAt: conv.ToPGTimestamptz(time.Now()),
			})
			require.NoError(t, err)
			principal, err := urn.ParsePrincipal(role.RoleUrn)
			require.NoError(t, err)
			selectors, err := authz.NewSelector(authz.ScopeMCPConnect, gatewayID.String()).MarshalJSON()
			require.NoError(t, err)
			_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
				OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors,
			})
			require.NoError(t, err)
			queries := pluginsrepo.New(ti.conn)
			plugin, err := queries.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Gateway delivery", Slug: "gateway-delivery"})
			require.NoError(t, err)
			_, err = queries.AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{OrganizationID: ac.ActiveOrganizationID, PluginID: plugin.ID, PrincipalUrn: role.RoleUrn})
			require.NoError(t, err)

			create := func(suffix string, rollback bool) {
				t.Helper()
				slug := ac.OrganizationSlug + "-gateway-" + suffix
				if transactional {
					tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: Exercise endpoint creation and rollback in the caller-owned transaction.
					require.NoError(t, err)
					defer func() { _ = tx.Rollback(ctx) }()
					_, err = ti.service.CreateMcpEndpointInTransaction(ctx, tx, mcpendpoints.CreateMcpEndpointInTransactionInput{AuthContext: ac, MetaMcpServerID: uuid.NullUUID{UUID: gatewayID, Valid: true}, Slug: slug})
					require.NoError(t, err)
					if rollback {
						members, err := pluginsrepo.New(tx).ListPluginServers(ctx, plugin.ID)
						require.NoError(t, err)
						require.Len(t, members, 1, "delivery occurs in the endpoint transaction")
						return
					}
					require.NoError(t, tx.Commit(ctx))
				} else {
					_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{MetaMcpServerID: conv.PtrEmpty(gatewayID.String()), Slug: types.McpEndpointSlug(slug)})
					require.NoError(t, err)
				}
			}
			if transactional {
				create("rolled-back", true)
				members, err := queries.ListPluginServers(ctx, plugin.ID)
				require.NoError(t, err)
				require.Empty(t, members, "rollback must not leave delivered membership")
			}
			create("first", false)
			members, err := queries.ListPluginServers(ctx, plugin.ID)
			require.NoError(t, err)
			require.Len(t, members, 1)
			require.Equal(t, gatewayID, members[0].MetaMcpServerID.UUID)
			_, err = queries.RemovePluginServer(ctx, pluginsrepo.RemovePluginServerParams{ID: members[0].ID, PluginID: plugin.ID})
			require.NoError(t, err)
			create("second", false)
			members, err = queries.ListPluginServers(ctx, plugin.ID)
			require.NoError(t, err)
			require.Empty(t, members, "another endpoint must preserve manual removal")
		})
	}
}
