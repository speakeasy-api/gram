package agentmanagement

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func upstreamWritesOff(ctx context.Context) context.Context {
	return authz.WithUpstreamSelectorWriteGate(ctx, func(context.Context, string) (bool, error) { return false, nil })
}

// While the organization's upstream_client write gate is off, nothing new
// names upstream_client: neither an owner block nor a manager's grant. Grants
// without the dimension, removing blocks and the protection existing blocks
// get from policy edits never depend on the gate.
func TestUpstreamSelectorWritesWaitForTheGate(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	const org = "org-upstream-gate"
	seedOrganization(t, conn, org)
	for _, user := range []string{"owner", "member"} {
		seedOrganizationUser(t, conn, org, user)
	}
	agent := createAgent(t, conn, org, "owner", "Gated agent")
	upstream := seedPersonalConnection(t, conn, org, "member")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	member := validatedHumanContext(t, org, "member")
	manager := validatedHumanContext(t, org, "owner")
	blockPayload := &gen.BlockAgentOnConnectionPayload{AgentID: agent.ID.String(), RemoteSessionClientID: upstream.String()}
	upstreamValue := upstream.String()

	_, err := service.BlockAgentOnConnection(upstreamWritesOff(member), blockPayload)
	requireCode(t, err, oops.CodeForbidden, "a new block waits for the gate")
	_, err = service.CreatePolicyGrant(upstreamWritesOff(manager), &gen.CreatePolicyGrantPayload{
		AgentID: agent.ID.String(), Scope: string(authz.ScopeMCPBlockedAuth), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCPAuthClient, ResourceID: "*", UpstreamClient: &upstreamValue},
	})
	requireCode(t, err, oops.CodeForbidden, "a manager's upstream_client grant waits for the gate")
	require.Empty(t, agentGrantRows(t, conn, org, agent.ID), "a refused write leaves nothing behind")

	block, err := service.BlockAgentOnConnection(member, blockPayload)
	require.NoError(t, err)
	other, err := service.CreatePolicyGrant(upstreamWritesOff(manager), &gen.CreatePolicyGrantPayload{
		AgentID: agent.ID.String(), Scope: string(authz.ScopeMCPBlockedAuth), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCPAuthClient, ResourceID: "client-one"},
	})
	require.NoError(t, err, "a grant without upstream_client never waits")

	grants, err := service.ListPolicyGrants(manager, &gen.ListPolicyGrantsPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.Len(t, grants, 2)
	var blockGrantID string
	for _, grant := range grants {
		if grant.ID != other.ID {
			blockGrantID = grant.ID
		}
	}
	requireCode(t, service.DeletePolicyGrant(upstreamWritesOff(manager), &gen.DeletePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: blockGrantID}), oops.CodeConflict, "the owner's block stays protected with the gate off")

	_, err = service.UpdatePolicyGrant(upstreamWritesOff(manager), &gen.UpdatePolicyGrantPayload{
		AgentID: agent.ID.String(), GrantID: other.ID, Scope: string(authz.ScopeMCPBlockedAuth), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCPAuthClient, ResourceID: "client-two"},
	})
	require.NoError(t, err, "ordinary policy updates keep working")
	require.NoError(t, service.DeletePolicyGrant(upstreamWritesOff(manager), &gen.DeletePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: other.ID}), "ordinary policy deletes keep working")
	rows := agentGrantRows(t, conn, org, agent.ID)
	require.Len(t, rows, 1, "the owner's block survives ordinary edits")
	selector, err := authz.SelectorFromRow(rows[0].Selectors)
	require.NoError(t, err)
	require.Equal(t, connectionBlockSelector(upstream, "member"), selector)

	require.NoError(t, service.RemoveConnectionBlock(upstreamWritesOff(member), &gen.RemoveConnectionBlockPayload{BlockID: block.ID}), "removing a block never waits")
	require.Empty(t, agentGrantRows(t, conn, org, agent.ID))
}
