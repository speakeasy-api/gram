package assistants

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestManagedAssistantOrganizationReadCeiling(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "managed_org_read")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "managed-org-read")
	core := newProvisioningCore(t, db)
	ordinary := createLegacyAssistant(t, db, project, "Ordinary assistant")
	upgraded, err := core.UpgradeAssistantIdentity(t.Context(), assistantidentity.ProvisionParams{OrganizationID: "org-test", ProjectID: project, AssistantID: ordinary.ID, ActorUserID: "user-1", Staff: nil})
	require.NoError(t, err)
	ordinaryGrants, err := agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: uuid.MustParse(*upgraded.AgentID)})
	require.NoError(t, err)
	for _, row := range ordinaryGrants {
		require.NotEqual(t, string(authz.ScopeOrgRead), row.Scope)
		require.NotEqual(t, string(authz.ScopeProjectRead), row.Scope)
	}

	_, err = core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1", true)
	require.NoError(t, err)
	// Hydrate the agent reference through the management read model.
	managed, err := core.GetManagedAssistant(t.Context(), project)
	require.NoError(t, err)
	require.NotNil(t, managed.AgentID)
	agentID := uuid.MustParse(*managed.AgentID)
	count := func(scope authz.Scope, allowedID, otherID string) int {
		rows, err := agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: agentID})
		require.NoError(t, err)
		n := 0
		for _, row := range rows {
			if row.Scope != string(scope) {
				continue
			}
			n++
			selector, err := authz.SelectorFromRow(row.Selectors)
			require.NoError(t, err)
			allowed, err := authz.GrantsAuthorize([]authz.Grant{authz.NewGrantWithSelector(scope, selector)}, authz.Check{Scope: scope, ResourceID: allowedID})
			require.NoError(t, err)
			require.True(t, allowed)
			allowed, err = authz.GrantsAuthorize([]authz.Grant{authz.NewGrantWithSelector(scope, selector)}, authz.Check{Scope: scope, ResourceID: otherID})
			require.NoError(t, err)
			require.False(t, allowed)
		}
		return n
	}
	countOrgRead := func() int { return count(authz.ScopeOrgRead, "org-test", "org_other") }
	countProjectRead := func() int { return count(authz.ScopeProjectRead, project.String(), uuid.NewString()) }
	require.Equal(t, 1, countOrgRead())
	require.Equal(t, 1, countProjectRead())
	_, err = core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1", true)
	require.NoError(t, err)
	require.Equal(t, 1, countOrgRead(), "repeated enable is idempotent")
	require.Equal(t, 1, countProjectRead(), "repeated enable is idempotent")
	rows, err := agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: agentID})
	require.NoError(t, err)
	for _, row := range rows {
		if row.Scope != string(authz.ScopeOrgRead) && row.Scope != string(authz.ScopeProjectRead) {
			continue
		}
		_, err = agentrepo.New(db).DeleteAgentPolicyGrant(t.Context(), agentrepo.DeleteAgentPolicyGrantParams{OrganizationID: "org-test", AgentID: agentID, GrantID: row.ID})
		require.NoError(t, err)
	}
	_, err = core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1", true)
	require.NoError(t, err)
	require.Equal(t, 1, countOrgRead(), "existing managed assistants gain the org ceiling")
	require.Equal(t, 1, countProjectRead(), "existing managed assistants gain the project ceiling")
	// A lapsed temporary grant for the same target is no ceiling: enabling
	// takes the row over and makes it permanent.
	rows, err = agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: agentID})
	require.NoError(t, err)
	for _, row := range rows {
		if row.Scope != string(authz.ScopeProjectRead) {
			continue
		}
		require.NoError(t, testrepo.New(db).LapsePrincipalGrantFixture(t.Context(), row.ID))
	}
	_, err = core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1", true)
	require.NoError(t, err)
	require.Equal(t, 1, countProjectRead(), "a lapsed temporary grant is replaced by the project ceiling")
	rows, err = agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: agentID})
	require.NoError(t, err)
	for _, row := range rows {
		if row.Scope == string(authz.ScopeProjectRead) {
			require.False(t, row.ExpiresAt.Valid, "the project ceiling is permanent")
		}
	}
	err = pgx.BeginFunc(t.Context(), db, func(tx pgx.Tx) error {
		return core.ensureManagedOrganizationRead(t.Context(), tx, "org-test", project, ordinary.ID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"))
	})
	require.ErrorIs(t, err, errManagedOrganizationReadTarget, "ordinary assistants cannot receive the managed ceiling")
}

func TestRepairManagedCeilingsAtTurnStart(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "managed_ceiling_repair")
	require.NoError(t, err)
	ctx := t.Context()
	project := newProvisioningProject(t, db, "managed-ceiling-repair")
	core := newProvisioningCore(t, db)
	_, err = core.EnableManagedAssistant(ctx, "org-test", project, "user-1", true)
	require.NoError(t, err)
	managed, err := core.GetManagedAssistant(ctx, project)
	require.NoError(t, err)
	require.NotNil(t, managed.AgentID)
	managedAgent := uuid.MustParse(*managed.AgentID)
	ordinary, err := core.CreateAssistant(ctx, "org-test", project, "user-1", "Ordinary assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, ordinary.AgentID)

	agents := agentrepo.New(db)
	ceilings := func(agentID uuid.UUID) map[string]int {
		rows, err := agents.ListAgentPolicyGrants(ctx, agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: agentID})
		require.NoError(t, err)
		counts := map[string]int{}
		for _, row := range rows {
			if row.Scope == string(authz.ScopeOrgRead) || row.Scope == string(authz.ScopeProjectRead) {
				counts[row.Scope]++
			}
		}
		return counts
	}

	// A managed assistant provisioned before the project ceiling existed holds
	// only the org ceiling.
	rows, err := agents.ListAgentPolicyGrants(ctx, agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: managedAgent})
	require.NoError(t, err)
	for _, row := range rows {
		if row.Scope != string(authz.ScopeProjectRead) {
			continue
		}
		_, err = agents.DeleteAgentPolicyGrant(ctx, agentrepo.DeleteAgentPolicyGrantParams{OrganizationID: "org-test", AgentID: managedAgent, GrantID: row.ID})
		require.NoError(t, err)
	}
	require.Equal(t, map[string]int{string(authz.ScopeOrgRead): 1}, ceilings(managedAgent))

	want := map[string]int{string(authz.ScopeOrgRead): 1, string(authz.ScopeProjectRead): 1}
	// Turn start runs the repair before anything else. The empty thread, event
	// and identity fail credential minting afterwards; only the repair matters.
	redisClient, err := assistantsInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	engine := authz.NewEngine(testenv.NewLogger(t), db, authztest.ChallengeLoggingAlwaysDisabled, nil, authz.EngineOpts{AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential, AdmitWorkloadSession: runtimepolicy.AdmitWorkloadSession})
	core.assistantTokens = assistanttokens.New("test-jwt-secret", db, engine, newTestPrincipalCredentials(t, db), cache.NewRedisCacheAdapter(redisClient))
	_, _ = core.mintTurnCredential(ctx, managed, assistantThreadRecord{}, assistantThreadEventRecord{}, turnIdentity{})
	require.Equal(t, want, ceilings(managedAgent), "the first turn repairs the missing project ceiling")
	repaired, err := agents.ListAgentPolicyGrants(ctx, agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: managedAgent})
	require.NoError(t, err)
	require.True(t, hasManagedCeiling(repaired, authz.NewGrant(authz.ScopeProjectRead, project.String())), "the repaired grant is permanent and covers this project")
	entry, err := audittest.LatestAuditLogByAction(ctx, db, audit.ActionAgentPolicyGrantCreate)
	require.NoError(t, err)
	require.Equal(t, managedCeilingRepairActor.String(), entry.ActorType+":"+entry.ActorID, "the repair is audited as the system repair actor")

	// A second process has not repaired this assistant yet, so it runs the
	// repair again and adds nothing.
	newProvisioningCore(t, db).repairManagedCeilings(ctx, managed)
	require.Equal(t, want, ceilings(managedAgent), "the repair is idempotent")

	core.repairManagedCeilings(ctx, ordinary)
	require.Empty(t, ceilings(uuid.MustParse(*ordinary.AgentID)), "ordinary assistants never receive the managed ceilings")
}
