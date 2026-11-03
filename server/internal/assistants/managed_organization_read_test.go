package assistants

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/authz"
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
	upgraded, err := core.UpgradeAssistantIdentity(t.Context(), assistantidentity.ProvisionParams{OrganizationID: "org-test", ProjectID: project, AssistantID: ordinary.ID, ActorUserID: "user-1", AgentID: uuid.Nil, AgentName: ""})
	require.NoError(t, err)
	ordinaryGrants, err := agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: uuid.MustParse(*upgraded.AgentID)})
	require.NoError(t, err)
	for _, row := range ordinaryGrants {
		require.NotEqual(t, string(authz.ScopeOrgRead), row.Scope)
	}

	_, err = core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1", true)
	require.NoError(t, err)
	// Hydrate the agent reference through the management read model.
	managed, err := core.GetManagedAssistant(t.Context(), project)
	require.NoError(t, err)
	require.NotNil(t, managed.AgentID)
	agentID := uuid.MustParse(*managed.AgentID)
	count := func() int {
		rows, err := agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: agentID})
		require.NoError(t, err)
		n := 0
		for _, row := range rows {
			if row.Scope != string(authz.ScopeOrgRead) {
				continue
			}
			n++
			selector, err := authz.SelectorFromRow(row.Selectors)
			require.NoError(t, err)
			allowed, err := authz.GrantsAuthorize([]authz.Grant{authz.NewGrantWithSelector(authz.ScopeOrgRead, selector)}, authz.Check{Scope: authz.ScopeOrgRead, ResourceID: "org-test"})
			require.NoError(t, err)
			require.True(t, allowed)
			allowed, err = authz.GrantsAuthorize([]authz.Grant{authz.NewGrantWithSelector(authz.ScopeOrgRead, selector)}, authz.Check{Scope: authz.ScopeOrgRead, ResourceID: "org_other"})
			require.NoError(t, err)
			require.False(t, allowed)
		}
		return n
	}
	require.Equal(t, 1, count())
	_, err = core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1", true)
	require.NoError(t, err)
	require.Equal(t, 1, count(), "repeated enable is idempotent")
	rows, err := agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: agentID})
	require.NoError(t, err)
	for _, row := range rows {
		if row.Scope != string(authz.ScopeOrgRead) {
			continue
		}
		_, err = agentrepo.New(db).DeleteAgentPolicyGrant(t.Context(), agentrepo.DeleteAgentPolicyGrantParams{OrganizationID: "org-test", AgentID: agentID, GrantID: row.ID})
		require.NoError(t, err)
	}
	_, err = core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1", true)
	require.NoError(t, err)
	require.Equal(t, 1, count(), "existing managed assistants gain the new ceiling")
	err = pgx.BeginFunc(t.Context(), db, func(tx pgx.Tx) error {
		return core.ensureManagedOrganizationRead(t.Context(), tx, "org-test", project, ordinary.ID, "user-1")
	})
	require.Error(t, err, "ordinary assistants cannot receive the managed ceiling")
}
