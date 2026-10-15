package agentmanagement

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAgentCreationEligibilityTargetsTheIntendedOwner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		scope   authz.Scope
		target  string
		owner   string
		allowed bool
	}{
		{name: "membership alone", owner: "caller"},
		{name: "write does not grant self creation", scope: authz.ScopeAgentWrite, target: "*", owner: "caller"},
		{name: "write does not grant provisioning", scope: authz.ScopeAgentWrite, target: "*", owner: "employee"},
		{name: "self", scope: authz.ScopeAgentCreate, target: authz.AgentOwnerSelf, owner: "caller", allowed: true},
		{name: "self cannot provision", scope: authz.ScopeAgentCreate, target: authz.AgentOwnerSelf, owner: "employee"},
		{name: "specified owner without recipient eligibility", scope: authz.ScopeAgentCreate, target: "employee", owner: "employee", allowed: true},
		{name: "specified owner is not self", scope: authz.ScopeAgentCreate, target: "employee", owner: "caller"},
		{name: "specified owner is not another employee", scope: authz.ScopeAgentCreate, target: "employee", owner: "other"},
		{name: "organization wide self", scope: authz.ScopeAgentCreate, target: "*", owner: "caller", allowed: true},
		{name: "organization wide employee", scope: authz.ScopeAgentCreate, target: "*", owner: "employee", allowed: true},
		{name: "cross organization", scope: authz.ScopeAgentCreate, target: "*", owner: "foreign"},
		{name: "targeted cross organization", scope: authz.ScopeAgentCreate, target: "foreign", owner: "foreign"},
		{name: "absent owner", scope: authz.ScopeAgentCreate, target: "*", owner: "absent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn := newTestDB(t)
			seedOrganization(t, conn, "org-create")
			seedOrganization(t, conn, "org-foreign")
			for _, user := range []string{"caller", "employee", "other"} {
				seedOrganizationUser(t, conn, "org-create", user)
			}
			seedOrganizationUser(t, conn, "org-foreign", "foreign")
			ctx := validatedHumanContext(t, "org-create", "caller")
			if tc.scope != "" {
				seedGrant(t, ctx, conn, "org-create", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), tc.scope, tc.target)
			}
			engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
			service := newTestService(conn, engine)
			created, err := service.Create(ctx, &gen.CreatePayload{Name: "New agent", OwnerUserID: new(tc.owner)})
			if !tc.allowed {
				requireOopsCode(t, err, oops.CodeForbidden)
				require.Nil(t, created)
				require.Empty(t, auditActions(t, conn, "org-create", ""))
				require.Empty(t, agentWebhookOutboxActions(t, conn, "org-create"))
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.owner, created.OwnerUserID)
			require.Equal(t, []string{"agent:create"}, auditActions(t, conn, "org-create", created.ID))
		})
	}
}

func TestRemovingCreationEligibilityPreservesOwnedAgentManagement(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-create")
	seedAgentCreator(t, conn, "org-create", "owner")
	ctx := validatedHumanContext(t, "org-create", "owner")
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := newTestService(conn, engine)
	created, err := service.Create(ctx, &gen.CreatePayload{Name: "Existing agent"})
	require.NoError(t, err)
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, "owner")
	selector, err := authz.NewSelector(authz.ScopeAgentCreate, authz.AgentOwnerSelf).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).DeletePrincipalGrantByIdentity(ctx, accessrepo.DeletePrincipalGrantByIdentityParams{
		OrganizationID: "org-create", PrincipalUrn: principal, Scope: string(authz.ScopeAgentCreate), Selectors: selector,
	})
	require.NoError(t, err)
	stale := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeAgentCreate, "*")})
	_, err = service.Create(stale, &gen.CreatePayload{Name: "Denied agent"})
	requireOopsCode(t, err, oops.CodeForbidden)
	got, err := service.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.True(t, got.Permissions.Write)
	require.True(t, got.Permissions.Authorize)
	_, err = service.Rename(ctx, &gen.RenamePayload{ID: created.ID, Name: "Still managed"})
	require.NoError(t, err)
	_, err = service.Suspend(ctx, &gen.SuspendPayload{AgentID: created.ID})
	require.NoError(t, err)
	require.Equal(t, []string{"agent:create", "agent:rename", "agent:suspend"}, auditActions(t, conn, "org-create", created.ID))
}
