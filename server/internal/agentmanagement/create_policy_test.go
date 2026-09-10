package agentmanagement

import (
	"context"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func initialMCPGrant() *gen.AgentPolicyGrantForm {
	return &gen.AgentPolicyGrantForm{Scope: string(authz.ScopeMCPRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: "*"}}
}

func TestCreateAgentWithInitialPolicy(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-create-policy")
	seedOrganizationUser(t, conn, "org-create-policy", "owner")
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := newTestService(conn, engine)
	service.features = &recordingAgentManagementFeatures{evaluation: feature.EvaluationEnabled}
	ctx := validatedHumanContext(t, "org-create-policy", "owner")
	created, err := service.Create(ctx, &gen.CreatePayload{Name: "Configured agent", PolicyGrants: []*gen.AgentPolicyGrantForm{initialMCPGrant()}})
	require.NoError(t, err)
	require.Equal(t, gen.AgentLifecycle("active"), created.Lifecycle)
	require.True(t, created.Permissions.Write)
	grants, err := service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: created.ID})
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, string(authz.ScopeMCPRead), grants[0].Scope)
	delegable, err := service.ListDelegableGrants(ctx, &gen.ListDelegableGrantsPayload{AgentID: created.ID})
	require.NoError(t, err)
	require.Empty(t, delegable)
	seedGrant(t, ctx, conn, "org-create-policy", urn.NewPrincipal(urn.PrincipalTypeUser, "owner"), authz.ScopeMCPRead, "*")
	delegable, err = service.ListDelegableGrants(ctx, &gen.ListDelegableGrantsPayload{AgentID: created.ID})
	require.NoError(t, err)
	// mcp:read implies mcp:connect.
	require.ElementsMatch(t, []*gen.AgentPolicyGrantForm{
		initialMCPGrant(),
		{Scope: string(authz.ScopeMCPConnect), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: "*"}},
	}, delegable)

	require.Equal(t, []string{"agent:create", "agent:policy_grant_create"}, agentWebhookOutboxActions(t, conn, "org-create-policy"))
	_, err = service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{AgentID: created.ID, Scope: grants[0].Scope, Effect: "allow", Selector: grants[0].Selector})
	requireOopsCode(t, err, oops.CodeConflict)
	for _, initial := range [][]*gen.AgentPolicyGrantForm{nil, {}} {
		created, err := service.Create(ctx, &gen.CreatePayload{Name: "Empty agent", PolicyGrants: initial})
		require.NoError(t, err)
		grants, err := service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: created.ID})
		require.NoError(t, err)
		require.Empty(t, grants)
		require.NoError(t, service.Delete(ctx, &gen.DeletePayload{AgentID: created.ID}))
	}
}

func TestCreateAgentInitialPolicyFailureIsAtomic(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name               string
		grants             []*gen.AgentPolicyGrantForm
		otherOwner         bool
		auditFailure       bool
		policyAuditFailure bool
	}{
		{name: "duplicate", grants: []*gen.AgentPolicyGrantForm{initialMCPGrant(), initialMCPGrant()}},
		{name: "nil", grants: []*gen.AgentPolicyGrantForm{initialMCPGrant(), nil}},
		{name: "unsafe", grants: []*gen.AgentPolicyGrantForm{initialMCPGrant(), {Scope: string(authz.ScopeAgentWrite), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: "agent", ResourceID: "*"}}}},
		{name: "deny", grants: []*gen.AgentPolicyGrantForm{initialMCPGrant(), {Scope: string(authz.ScopeMCPRead), Effect: "deny", Selector: &gen.AgentPolicySelector{ResourceKind: "mcp", ResourceID: "*"}}}},
		{name: "selector", grants: []*gen.AgentPolicyGrantForm{initialMCPGrant(), {Scope: string(authz.ScopeMCPRead), Effect: "allow"}}},
		{name: "other owner", grants: []*gen.AgentPolicyGrantForm{initialMCPGrant()}, otherOwner: true},
		{name: "audit", grants: []*gen.AgentPolicyGrantForm{initialMCPGrant()}, auditFailure: true},
		{name: "policy audit", grants: []*gen.AgentPolicyGrantForm{initialMCPGrant()}, policyAuditFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			conn := newTestDB(t)
			seedOrganization(t, conn, "org-create-policy")
			seedOrganizationUser(t, conn, "org-create-policy", "owner")
			seedOrganizationUser(t, conn, "org-create-policy", "other")
			service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
			ctx := validatedHumanContext(t, "org-create-policy", "owner")
			payload := &gen.CreatePayload{Name: "Failed agent", PolicyGrants: test.grants}
			if test.otherOwner {
				owner := "other"
				payload.OwnerUserID = &owner
			}
			if test.auditFailure {
				require.NoError(t, testrepo.New(conn).RejectPublishOutboxWritesFixture(ctx))
			}
			if test.policyAuditFailure {
				require.NoError(t, testrepo.New(conn).RejectAgentPolicyGrantAuditWritesFixture(ctx))
			}
			_, err := service.Create(ctx, payload)
			require.Error(t, err)
			if test.policyAuditFailure {
				var pgerr *pgconn.PgError
				require.ErrorAs(t, err, &pgerr)
				require.Equal(t, "reject_agent_policy_grant_audit_fixture", pgerr.ConstraintName)
			}
			if test.otherOwner {
				requireOopsCode(t, err, oops.CodeForbidden)
			}
			var agents, grants, audits int
			//nolint:glint // notestingrawsql: directly verifies atomic rollback of agent, grants and audit rows
			err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM agents WHERE organization_id = $1), (SELECT count(*) FROM principal_grants WHERE organization_id = $1), (SELECT count(*) FROM audit_logs WHERE organization_id = $1)`, "org-create-policy").Scan(&agents, &grants, &audits)
			require.NoError(t, err)
			require.Zero(t, agents)
			require.Zero(t, grants)
			require.Zero(t, audits)
			require.Empty(t, agentWebhookOutboxActions(t, conn, "org-create-policy"))
		})
	}
}

func TestCreateAgentInitialPolicyForOtherOwnerRequiresTenantMembership(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-create-policy")
	seedOrganization(t, conn, "org-other-policy")
	seedOrganizationUser(t, conn, "org-create-policy", "caller")
	seedOrganizationUser(t, conn, "org-create-policy", "owner")
	seedOrganizationUser(t, conn, "org-other-policy", "outside")
	ctx := validatedHumanContext(t, "org-create-policy", "caller")
	seedGrant(t, ctx, conn, "org-create-policy", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), authz.ScopeAgentWrite, "*")
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := newTestService(conn, engine)
	owner := "owner"
	created, err := service.Create(ctx, &gen.CreatePayload{Name: "Other owned agent", OwnerUserID: &owner, PolicyGrants: []*gen.AgentPolicyGrantForm{initialMCPGrant()}})
	require.NoError(t, err)
	grants, err := service.ListPolicyGrants(validatedHumanContext(t, "org-create-policy", owner), &gen.ListPolicyGrantsPayload{AgentID: created.ID})
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, string(authz.ScopeMCPRead), grants[0].Scope)
	owner = "outside"
	_, err = service.Create(ctx, &gen.CreatePayload{Name: "Cross tenant agent", OwnerUserID: &owner, PolicyGrants: []*gen.AgentPolicyGrantForm{initialMCPGrant()}})
	requireOopsCode(t, err, oops.CodeForbidden)
	var agents, policies int
	//nolint:glint // notestingrawsql: verifies failed cross-tenant creation did not persist agent or policy
	err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM agents WHERE organization_id = $1), (SELECT count(*) FROM principal_grants WHERE organization_id = $1 AND principal_urn LIKE 'agent:%')`, "org-create-policy").Scan(&agents, &policies)
	require.NoError(t, err)
	require.Equal(t, 1, agents)
	require.Equal(t, 1, policies)
	require.Equal(t, []string{"agent:create", "agent:policy_grant_create"}, agentWebhookOutboxActions(t, conn, "org-create-policy"))
}
