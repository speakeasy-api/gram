package agentmanagement

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAgentPolicyWritesTakeOverProvisionedGrants(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-takeover")
	seedOrganizationUser(t, conn, "org-takeover", "owner")
	agent := createAgent(t, conn, "org-takeover", "owner", "Takeover agent")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-takeover", "owner")
	seedGrant(t, ctx, conn, "org-takeover", urn.NewPrincipal(urn.PrincipalTypeUser, "owner"), authz.ScopeProjectWrite, "*")

	selector, err := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindProject, authz.SelectorKeyResourceID: "project-one"}.MarshalJSON()
	require.NoError(t, err)
	agentURN := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String()
	require.NoError(t, accessrepo.New(conn).RecordAuthPermissionDefault(t.Context(), accessrepo.RecordAuthPermissionDefaultParams{
		OrganizationID: "org-takeover", PrincipalUrn: agentURN, Scope: string(authz.ScopeProjectWrite), Selectors: selector, Kind: "shared_client",
	}))

	_, err = service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{
		AgentID: agent.ID.String(), Scope: string(authz.ScopeProjectWrite), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "project-one"},
	})
	require.NoError(t, err)

	rows, err := authclientbackfill.New(conn).ListAuthPermissionProvisionsFixture(t.Context(), "org-takeover")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].TakenOver, "an owner's explicit write makes the grant theirs, so a rollback leaves it")
}

func TestAgentPolicyUpdateTakesOverProvisionedGrant(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-takeover-update")
	seedOrganizationUser(t, conn, "org-takeover-update", "owner")
	agent := createAgent(t, conn, "org-takeover-update", "owner", "Takeover update agent")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-takeover-update", "owner")
	seedGrant(t, ctx, conn, "org-takeover-update", urn.NewPrincipal(urn.PrincipalTypeUser, "owner"), authz.ScopeProjectWrite, "*")

	created, err := service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{
		AgentID: agent.ID.String(), Scope: string(authz.ScopeProjectWrite), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "project-two"},
	})
	require.NoError(t, err)

	selector, err := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindProject, authz.SelectorKeyResourceID: "project-one"}.MarshalJSON()
	require.NoError(t, err)
	agentURN := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String()
	require.NoError(t, accessrepo.New(conn).RecordAuthPermissionDefault(t.Context(), accessrepo.RecordAuthPermissionDefaultParams{
		OrganizationID: "org-takeover-update", PrincipalUrn: agentURN, Scope: string(authz.ScopeProjectWrite), Selectors: selector, Kind: "shared_client",
	}))

	_, err = service.UpdatePolicyGrant(ctx, &gen.UpdatePolicyGrantPayload{
		AgentID: agent.ID.String(), GrantID: created.ID, Scope: string(authz.ScopeProjectWrite), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "project-one"},
	})
	require.NoError(t, err)

	rows, err := authclientbackfill.New(conn).ListAuthPermissionProvisionsFixture(t.Context(), "org-takeover-update")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].TakenOver, "updating a grant onto a provisioned one takes it over")
}
