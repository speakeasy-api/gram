package agentmanagement

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestOwnerScopedWriteCreatesAndManagesOnlySelectedOwners(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-owner-write")
	for _, user := range []string{"caller", "employee", "other"} {
		seedOrganizationUser(t, db, "org-owner-write", user)
	}
	ctx := validatedHumanContext(t, "org-owner-write", "caller")
	seedGrant(t, ctx, db, "org-owner-write", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), authz.ScopeAgentWrite, "*", "employee")
	engine := authz.NewEngine(testenv.NewLogger(t), db, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := newTestService(db, engine)
	agent, err := service.Create(ctx, &gen.CreatePayload{Name: "Provisioned agent", OwnerUserID: new("employee")})
	require.NoError(t, err)
	require.True(t, agent.Permissions.Read)
	require.True(t, agent.Permissions.Write)
	require.False(t, agent.Permissions.Authorize)
	require.False(t, agent.Permissions.Transfer)
	_, err = service.Rename(ctx, &gen.RenamePayload{ID: agent.ID, Name: "Managed by delegate"})
	require.NoError(t, err)
	other := createAgent(t, db, "org-owner-write", "other", "Another owner's agent")
	_, err = service.Rename(ctx, &gen.RenamePayload{ID: other.ID.String(), Name: "Denied edit"})
	requireOopsCode(t, err, oops.CodeForbidden)
	listed, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, agent.ID, listed[0].ID)

	// The recipient has no grants: intrinsic ownership still permits transfer.
	ownerCtx := validatedHumanContext(t, "org-owner-write", "employee")
	_, err = service.Transfer(ownerCtx, &gen.TransferPayload{AgentID: agent.ID, OwnerUserID: "other"})
	require.NoError(t, err)
	_, err = service.Rename(ctx, &gen.RenamePayload{ID: agent.ID, Name: "Old owner grant cannot follow transfer"})
	requireOopsCode(t, err, oops.CodeForbidden)
	newOwnerCtx := validatedHumanContext(t, "org-owner-write", "other")
	_, err = service.Rename(newOwnerCtx, &gen.RenamePayload{ID: agent.ID, Name: "Managed by new owner without grants"})
	require.NoError(t, err)
}

func TestExactAgentGrantCannotCreateAndRetainsOwnerRestriction(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-exact-write")
	for _, user := range []string{"caller", "employee", "other"} {
		seedOrganizationUser(t, db, "org-exact-write", user)
	}
	agent := createAgent(t, db, "org-exact-write", "employee", "Existing agent")
	ctx := validatedHumanContext(t, "org-exact-write", "caller")
	seedGrant(t, ctx, db, "org-exact-write", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), authz.ScopeAgentWrite, agent.ID.String(), "other")
	engine := authz.NewEngine(testenv.NewLogger(t), db, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := newTestService(db, engine)
	_, err := service.Rename(ctx, &gen.RenamePayload{ID: agent.ID.String(), Name: "Wrong owner"})
	requireOopsCode(t, err, oops.CodeForbidden)
	seedGrant(t, ctx, db, "org-exact-write", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), authz.ScopeAgentWrite, agent.ID.String(), "employee")
	_, err = service.Rename(ctx, &gen.RenamePayload{ID: agent.ID.String(), Name: "Selected agent"})
	require.NoError(t, err)
	_, err = service.Create(ctx, &gen.CreatePayload{Name: "Cannot create", OwnerUserID: new("employee")})
	requireOopsCode(t, err, oops.CodeForbidden)
}
