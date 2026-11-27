package admin

import (
	"maps"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func builtinGrantIDsBySlug(t *testing.T, rows []agentrepo.ListAgentPolicyGrantsRow, project uuid.UUID) map[string]agentrepo.ListAgentPolicyGrantsRow {
	t.Helper()
	found := make(map[string]agentrepo.ListAgentPolicyGrantsRow)
	for _, slug := range []string{
		platformtools.AssistantsPlatformToolsetSlug,
		platformtools.PlatformMCPReadToolsetSlug,
		platformtools.SpeakeasySlackPlatformToolsetSlug,
	} {
		want := authz.NewSelector(authz.ScopeMCPConnect, platformtools.PlatformToolsetID(slug).String())
		want[authz.SelectorKeyProjectID] = project.String()
		for _, row := range rows {
			if row.Scope != string(authz.ScopeMCPConnect) {
				continue
			}
			got, err := authz.SelectorFromRow(row.Selectors)
			require.NoError(t, err)
			if maps.Equal(got, want) {
				found[slug] = row
				break
			}
		}
	}
	return found
}

func deleteAllBuiltinGrants(t *testing.T, agents *agentrepo.Queries, orgID string, agentID, project uuid.UUID) {
	t.Helper()
	rows, err := agents.ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{
		OrganizationID: orgID,
		AgentID:        agentID,
	})
	require.NoError(t, err)
	for _, row := range builtinGrantIDsBySlug(t, rows, project) {
		_, err := agents.DeleteAgentPolicyGrant(t.Context(), agentrepo.DeleteAgentPolicyGrantParams{
			OrganizationID: orgID,
			AgentID:        agentID,
			GrantID:        row.ID,
		})
		require.NoError(t, err)
	}
}

func deleteAllMCPConnectGrants(t *testing.T, agents *agentrepo.Queries, orgID string, agentID uuid.UUID) {
	t.Helper()
	rows, err := agents.ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{
		OrganizationID: orgID,
		AgentID:        agentID,
	})
	require.NoError(t, err)
	for _, row := range rows {
		if row.Scope != string(authz.ScopeMCPConnect) {
			continue
		}
		_, err := agents.DeleteAgentPolicyGrant(t.Context(), agentrepo.DeleteAgentPolicyGrantParams{
			OrganizationID: orgID,
			AgentID:        agentID,
			GrantID:        row.ID,
		})
		require.NoError(t, err)
	}
}

func TestBackfillOrganizationAssistantBuiltinGrants(t *testing.T) {
	t.Parallel()
	svc, db := newAssistantMigrationService(t)
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{
		SessionID:   "staff-session",
		OIDCSubject: "staff-subject",
		Email:       "staff@example.invalid",
		Name:        "Staff Member",
		HD:          testAdminHD,
	})

	org := newAssistantMigrationOrg(t, db, "assistant-builtin-backfill")
	owner := org.member(true)
	withWildcard := org.legacyAssistant("Wildcard only", owner)
	narrowed := org.legacyAssistant("Narrowed policy", owner)
	managed := org.legacyAssistant("Managed assistant", owner)
	require.NoError(t, assistantrepo.New(db).CreateProjectManagedAssistant(t.Context(), assistantrepo.CreateProjectManagedAssistantParams{
		ProjectID:   org.project,
		AssistantID: managed,
	}))
	_, err := svc.MigrateOrganizationAssistantIdentities(ctx, &gen.MigrateOrganizationAssistantIdentitiesPayload{OrganizationID: org.id})
	require.NoError(t, err)
	// Create an unbound assistant after migration to verify the backfill ignores it.
	unbound := org.legacyAssistant("Unbound assistant", owner)

	other := newAssistantMigrationOrg(t, db, "assistant-builtin-other")
	foreign := other.legacyAssistant("Foreign assistant", other.member(true))
	_, err = svc.MigrateOrganizationAssistantIdentities(ctx, &gen.MigrateOrganizationAssistantIdentitiesPayload{OrganizationID: other.id})
	require.NoError(t, err)

	states, err := assistantidentity.States(t.Context(), db, org.project, []uuid.UUID{withWildcard, narrowed, managed, unbound})
	require.NoError(t, err)
	foreignStates, err := assistantidentity.States(t.Context(), db, other.project, []uuid.UUID{foreign})
	require.NoError(t, err)

	agents := agentrepo.New(db)
	// Seed assistants missing explicit built-in grants:
	// - withWildcard keeps project-wide mcp:connect * but lacks explicit built-in row.
	// - narrowed has no mcp:connect grants at all.
	// - managed has a lapsed temporary assistants grant and lacks platform/speakeasy-slack.
	// - foreign lacks its explicit built-in row and must stay untouched.
	deleteAllBuiltinGrants(t, agents, org.id, *states[withWildcard].AgentID, org.project)
	deleteAllMCPConnectGrants(t, agents, org.id, *states[narrowed].AgentID)
	managedRows, err := agents.ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{
		OrganizationID: org.id,
		AgentID:        *states[managed].AgentID,
	})
	require.NoError(t, err)
	managedBuiltins := builtinGrantIDsBySlug(t, managedRows, org.project)
	require.NoError(t, testrepo.New(db).LapsePrincipalGrantFixture(t.Context(), managedBuiltins[platformtools.AssistantsPlatformToolsetSlug].ID))
	for _, slug := range []string{platformtools.PlatformMCPReadToolsetSlug, platformtools.SpeakeasySlackPlatformToolsetSlug} {
		_, err := agents.DeleteAgentPolicyGrant(t.Context(), agentrepo.DeleteAgentPolicyGrantParams{
			OrganizationID: org.id,
			AgentID:        *states[managed].AgentID,
			GrantID:        managedBuiltins[slug].ID,
		})
		require.NoError(t, err)
	}
	deleteAllBuiltinGrants(t, agents, other.id, *foreignStates[foreign].AgentID, other.project)

	auditCreatesBefore, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionAgentPolicyGrantCreate)
	require.NoError(t, err)
	auditUpdatesBefore, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionAgentPolicyGrantUpdate)
	require.NoError(t, err)

	run, err := svc.BackfillOrganizationAssistantBuiltinGrants(ctx, &gen.BackfillOrganizationAssistantBuiltinGrantsPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Equal(t, org.id, run.OrganizationID)
	require.Equal(t, int64(3), run.Granted)
	require.Zero(t, run.AlreadyGranted)
	require.Zero(t, run.Failed)
	require.Len(t, run.Results, 3)

	byAssistant := make(map[string]*gen.AdminAssistantBuiltinGrantBackfillResult, len(run.Results))
	for _, res := range run.Results {
		byAssistant[res.AssistantID] = res
	}
	require.Equal(t, assistantBuiltinGrantsGranted, byAssistant[withWildcard.String()].Outcome)
	require.Equal(t, int64(1), byAssistant[withWildcard.String()].GrantsAdded)
	require.Equal(t, assistantBuiltinGrantsGranted, byAssistant[narrowed.String()].Outcome)
	require.Equal(t, int64(1), byAssistant[narrowed.String()].GrantsAdded)
	require.Equal(t, assistantBuiltinGrantsGranted, byAssistant[managed.String()].Outcome)
	require.Equal(t, int64(3), byAssistant[managed.String()].GrantsAdded)
	require.NotContains(t, byAssistant, unbound.String())

	// Verify exact permanent grants exist on every agent in org.
	for _, id := range []uuid.UUID{withWildcard, narrowed} {
		rows, err := agents.ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{
			OrganizationID: org.id,
			AgentID:        *states[id].AgentID,
		})
		require.NoError(t, err)
		got := builtinGrantIDsBySlug(t, rows, org.project)
		require.Contains(t, got, platformtools.AssistantsPlatformToolsetSlug)
		require.False(t, got[platformtools.AssistantsPlatformToolsetSlug].ExpiresAt.Valid)
		require.NotContains(t, got, platformtools.PlatformMCPReadToolsetSlug)
		require.NotContains(t, got, platformtools.SpeakeasySlackPlatformToolsetSlug)
	}
	managedAfter, err := agents.ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{
		OrganizationID: org.id,
		AgentID:        *states[managed].AgentID,
	})
	require.NoError(t, err)
	gotManaged := builtinGrantIDsBySlug(t, managedAfter, org.project)
	for _, slug := range []string{
		platformtools.AssistantsPlatformToolsetSlug,
		platformtools.PlatformMCPReadToolsetSlug,
		platformtools.SpeakeasySlackPlatformToolsetSlug,
	} {
		require.Contains(t, gotManaged, slug)
		require.False(t, gotManaged[slug].ExpiresAt.Valid, "backfilled grant for %s must be permanent", slug)
	}

	// Foreign org's assistant was not touched.
	foreignRows, err := agents.ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{
		OrganizationID: other.id,
		AgentID:        *foreignStates[foreign].AgentID,
	})
	require.NoError(t, err)
	require.Empty(t, builtinGrantIDsBySlug(t, foreignRows, other.project))

	// Four newly created grants and one promoted temporary grant are audited as
	// staff create and update events respectively.
	auditCreatesAfterFirst, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionAgentPolicyGrantCreate)
	require.NoError(t, err)
	require.Equal(t, auditCreatesBefore+4, auditCreatesAfterFirst)
	auditUpdatesAfterFirst, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionAgentPolicyGrantUpdate)
	require.NoError(t, err)
	require.Equal(t, auditUpdatesBefore+1, auditUpdatesAfterFirst)
	latest, err := audittest.LatestAuditLogByActionAndSubject(t.Context(), db, audit.ActionAgentPolicyGrantCreate, states[managed].AgentID.String())
	require.NoError(t, err)
	require.Equal(t, "staff-subject", latest.ActorID)
	latestUpdate, err := audittest.LatestAuditLogByActionAndSubject(t.Context(), db, audit.ActionAgentPolicyGrantUpdate, states[managed].AgentID.String())
	require.NoError(t, err)
	require.Equal(t, "staff-subject", latestUpdate.ActorID)

	// Re-running is idempotent and writes no additional grants or audit entries.
	again, err := svc.BackfillOrganizationAssistantBuiltinGrants(ctx, &gen.BackfillOrganizationAssistantBuiltinGrantsPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Zero(t, again.Granted)
	require.Equal(t, int64(3), again.AlreadyGranted)
	require.Zero(t, again.Failed)
	for _, res := range again.Results {
		require.Equal(t, assistantBuiltinGrantsAlreadyGranted, res.Outcome)
		require.Zero(t, res.GrantsAdded)
	}
	auditCreatesAfterSecond, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionAgentPolicyGrantCreate)
	require.NoError(t, err)
	require.Equal(t, auditCreatesAfterFirst, auditCreatesAfterSecond)
	auditUpdatesAfterSecond, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionAgentPolicyGrantUpdate)
	require.NoError(t, err)
	require.Equal(t, auditUpdatesAfterFirst, auditUpdatesAfterSecond)

	// Missing organization returns not found.
	_, err = svc.BackfillOrganizationAssistantBuiltinGrants(ctx, &gen.BackfillOrganizationAssistantBuiltinGrantsPayload{OrganizationID: "org_missing_" + uuid.NewString()})
	var notFound *oops.ShareableError
	require.ErrorAs(t, err, &notFound)
	require.Equal(t, oops.CodeNotFound, notFound.Code)
}

func TestBackfillOrganizationAssistantBuiltinGrantsConcurrent(t *testing.T) {
	t.Parallel()
	svc, db := newAssistantMigrationService(t)
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{
		SessionID:   "staff-session",
		OIDCSubject: "staff-subject",
		Email:       "staff@example.invalid",
		Name:        "Staff Member",
		HD:          testAdminHD,
	})
	org := newAssistantMigrationOrg(t, db, "assistant-builtin-concurrent")
	id := org.legacyAssistant("Concurrent", org.member(true))
	_, err := svc.MigrateOrganizationAssistantIdentities(ctx, &gen.MigrateOrganizationAssistantIdentitiesPayload{OrganizationID: org.id})
	require.NoError(t, err)
	states, err := assistantidentity.States(t.Context(), db, org.project, []uuid.UUID{id})
	require.NoError(t, err)
	deleteAllBuiltinGrants(t, agentrepo.New(db), org.id, *states[id].AgentID, org.project)

	type result struct {
		run *gen.AdminAssistantBuiltinGrantBackfillRun
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			run, err := svc.BackfillOrganizationAssistantBuiltinGrants(ctx, &gen.BackfillOrganizationAssistantBuiltinGrantsPayload{OrganizationID: org.id})
			results <- result{run: run, err: err}
		}()
	}
	var granted, alreadyGranted int64
	for range 2 {
		res := <-results
		require.NoError(t, res.err)
		require.Zero(t, res.run.Failed)
		granted += res.run.Granted
		alreadyGranted += res.run.AlreadyGranted
	}
	require.Equal(t, int64(1), granted, "concurrent backfills grant and audit at most once")
	require.Equal(t, int64(1), alreadyGranted)
}
