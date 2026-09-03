package agentmanagement

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestServiceLifecycleMutationsAreAuditedAtomically(t *testing.T) {
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")

	created, err := service.Create(ctx, &gen.CreatePayload{Name: "  Build agent  "})
	require.NoError(t, err)
	require.Equal(t, "Build agent", created.Name)
	require.Equal(t, gen.AgentLifecycle("active"), created.Lifecycle)
	require.True(t, created.Permissions.Read)
	require.True(t, created.Permissions.Write)
	require.True(t, created.Permissions.Authorize)
	require.True(t, created.Permissions.Transfer)

	read, err := service.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, created.ID, read.ID)

	renamed, err := service.Rename(ctx, &gen.RenamePayload{ID: created.ID, Name: "Renamed agent"})
	require.NoError(t, err)
	require.Equal(t, "Renamed agent", renamed.Name)

	suspended, err := service.Suspend(ctx, &gen.SuspendPayload{AgentID: created.ID})
	require.NoError(t, err)
	require.Equal(t, gen.AgentLifecycle("suspended"), suspended.Lifecycle)

	resumed, err := service.Resume(ctx, &gen.ResumePayload{AgentID: created.ID})
	require.NoError(t, err)
	require.Equal(t, gen.AgentLifecycle("active"), resumed.Lifecycle)

	revoked, err := service.Revoke(ctx, &gen.RevokePayload{AgentID: created.ID})
	require.NoError(t, err)
	require.Equal(t, gen.AgentLifecycle("revoked"), revoked.Lifecycle)

	_, err = service.Resume(ctx, &gen.ResumePayload{AgentID: created.ID})
	requireOopsCode(t, err, oops.CodeConflict)

	err = service.Delete(ctx, &gen.DeletePayload{AgentID: created.ID})
	require.NoError(t, err)
	_, err = service.Get(ctx, &gen.GetPayload{ID: created.ID})
	requireOopsCode(t, err, oops.CodeForbidden)

	rows, err := conn.Query(t.Context(), `SELECT action FROM audit_logs WHERE organization_id = $1 AND subject_id = $2 ORDER BY seq`, "org-a", created.ID)
	require.NoError(t, err)
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action string
		require.NoError(t, rows.Scan(&action))
		actions = append(actions, action)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{
		"agent:create",
		"agent:rename",
		"agent:suspend",
		"agent:resume",
		"agent:revoke",
		"agent:delete",
	}, actions)
}

func TestFailedLifecycleMutationDoesNotEmitAudit(t *testing.T) {
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")
	created, err := service.Create(ctx, &gen.CreatePayload{Name: "Lifecycle agent"})
	require.NoError(t, err)

	_, err = service.Resume(ctx, &gen.ResumePayload{AgentID: created.ID})
	requireOopsCode(t, err, oops.CodeConflict)

	var count int
	err = conn.QueryRow(t.Context(), `SELECT count(*) FROM audit_logs WHERE organization_id = $1 AND subject_id = $2`, "org-a", created.ID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestNameConflictIsScopedToActiveOrganizationAgents(t *testing.T) {
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "owner")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")

	first, err := service.Create(ctx, &gen.CreatePayload{Name: "Case Name"})
	require.NoError(t, err)
	_, err = service.Create(ctx, &gen.CreatePayload{Name: "case name"})
	requireOopsCode(t, err, oops.CodeConflict)
	require.NoError(t, service.Delete(ctx, &gen.DeletePayload{AgentID: first.ID}))
	_, err = service.Create(ctx, &gen.CreatePayload{Name: "case name"})
	require.NoError(t, err)
}
