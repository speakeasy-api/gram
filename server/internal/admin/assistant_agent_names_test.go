package admin

import (
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRepairAssistantAgentNames(t *testing.T) {
	t.Parallel()
	svc, db := newAssistantMigrationService(t)
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{OIDCSubject: "staff-subject", Name: "Staff Member"})
	org := newAssistantMigrationOrg(t, db, "assistant-name-repair")
	owner := org.member(true)
	repair := org.legacyAssistant("Repair", owner)
	custom := org.legacyAssistant("Custom", owner)
	duplicate := org.legacyAssistant("Duplicate", owner)
	otherProject := newAssistantMigrationOrg(t, db, "assistant-name-other")
	// A second project in the same organization may have an identically named assistant.
	_, err := db.Exec(ctx, `UPDATE projects SET organization_id=$1 WHERE id=$2`, org.id, otherProject.project) //nolint:glint // notestingrawsql: corruption/edge-case fixture: two projects in one organization.
	require.NoError(t, err)
	otherProject.id = org.id
	peer := otherProject.legacyAssistant("duplicate", owner)
	unbound := org.legacyAssistant("Unbound", owner)
	_, err = svc.MigrateOrganizationAssistantIdentities(ctx, &gen.MigrateOrganizationAssistantIdentitiesPayload{OrganizationID: org.id})
	require.NoError(t, err)
	// Seed the generated names that existing installations may still hold.
	for _, id := range []uuid.UUID{repair, duplicate, peer} {
		_, err = db.Exec(ctx, `UPDATE agents g SET name='Assistant ' || $1::text FROM assistant_agent_bindings b WHERE b.assistant_id=$1::uuid AND g.id=b.agent_id`, id.String()) //nolint:glint // notestingrawsql: legacy generated-name fixture.
		require.NoError(t, err)
	}
	_, err = db.Exec(ctx, `UPDATE agents g SET name='Hand chosen' FROM assistant_agent_bindings b WHERE b.assistant_id=$1 AND g.id=b.agent_id`, custom) //nolint:glint // notestingrawsql: custom-name preservation fixture.
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE assistant_agent_bindings SET deleted_at=clock_timestamp() WHERE assistant_id=$1`, unbound) //nolint:glint // notestingrawsql: unbound assistant fixture.
	require.NoError(t, err)
	listed, err := svc.ListOrganizationAssistants(ctx, &gen.ListOrganizationAssistantsPayload{OrganizationID: org.id})
	require.NoError(t, err)
	states := map[string]string{}
	for _, a := range listed.Assistants {
		states[a.ID] = a.AgentNameRepairState
	}
	require.Equal(t, "pending", states[repair.String()])
	require.Equal(t, "conflict", states[duplicate.String()])
	require.Equal(t, "conflict", states[peer.String()])
	require.Equal(t, "not_needed", states[custom.String()])
	require.Equal(t, "not_needed", states[unbound.String()])
	run, err := svc.RepairOrganizationAssistantAgentNames(ctx, &gen.RepairOrganizationAssistantAgentNamesPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Equal(t, int64(1), run.Repaired)
	require.Equal(t, int64(2), run.Conflicts)
	require.Zero(t, run.Failed)
	again, err := svc.RepairOrganizationAssistantAgentNames(ctx, &gen.RepairOrganizationAssistantAgentNamesPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Zero(t, again.Repaired)
	require.Equal(t, int64(2), again.Conflicts)
	organizations, err := svc.ListAssistantOrganizations(ctx, &gen.ListAssistantOrganizationsPayload{})
	require.NoError(t, err)
	found := false
	for _, o := range organizations.Organizations {
		if o.ID == org.id {
			require.Equal(t, int64(5), o.Assistants)
			found = true
		}
	}
	require.True(t, found)
}

func TestRepairAssistantAgentNamesConcurrentAndSavepoint(t *testing.T) {
	t.Parallel()
	svc, db := newAssistantMigrationService(t)
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{OIDCSubject: "staff-subject"})
	org := newAssistantMigrationOrg(t, db, "assistant-name-concurrent")
	id := org.legacyAssistant("Repair", org.member(true))
	_, err := svc.MigrateOrganizationAssistantIdentities(ctx, &gen.MigrateOrganizationAssistantIdentitiesPayload{OrganizationID: org.id})
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE agents g SET name='Assistant ' || $1::text FROM assistant_agent_bindings b WHERE b.assistant_id=$1::uuid AND g.id=b.agent_id`, id.String()) //nolint:glint // notestingrawsql: legacy-name fixture.
	require.NoError(t, err)
	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary for savepoint recovery test.
	require.NoError(t, err)
	rows, err := repo.New(tx).AdminListAssistantAgentNames(ctx, org.id)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	invalid := rows[0]
	invalid.Name = ""
	require.Error(t, repairAssistantAgentName(ctx, tx, svc.audit, org.id, invalid))
	rows, err = repo.New(tx).AdminListAssistantAgentNames(ctx, org.id)
	require.NoError(t, err, "savepoint rollback preserves parent transaction")
	require.Equal(t, "Assistant "+id.String(), rows[0].AgentName)
	require.NoError(t, tx.Commit(ctx))
	type result struct {
		run *gen.AdminAssistantAgentNameRepairRun
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			run, err := svc.RepairOrganizationAssistantAgentNames(ctx, &gen.RepairOrganizationAssistantAgentNamesPayload{OrganizationID: org.id})
			results <- result{run: run, err: err}
		}()
	}
	var repaired int64
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		require.Zero(t, result.run.Failed)
		repaired += result.run.Repaired
	}
	require.Equal(t, int64(1), repaired, "concurrent requests rename and audit at most once")
}

// Disabled accounts remain visible to staff migration operators. Reading or
// migrating assistants must not implicitly enable their organization.
func TestListAssistantOrganizationsIncludesDisabled(t *testing.T) {
	t.Parallel()
	svc, db := newAssistantMigrationService(t)
	org := newAssistantMigrationOrg(t, db, "disabled-assistants")
	org.legacyAssistant("Pending assistant", org.member(true))
	count, err := repo.New(db).AdminDisableOrganization(t.Context(), org.id)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	newAssistantMigrationOrg(t, db, "empty-assistants")
	listed, err := svc.ListAssistantOrganizations(t.Context(), &gen.ListAssistantOrganizationsPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Organizations, 1)
	require.Equal(t, org.id, listed.Organizations[0].ID)
	require.EqualValues(t, 1, listed.Organizations[0].Assistants)
	stored, err := repo.New(db).AdminGetOrganization(t.Context(), repo.AdminGetOrganizationParams{ID: org.id, AllowSlug: false})
	require.NoError(t, err)
	require.True(t, stored.DisabledAt.Valid)
}
