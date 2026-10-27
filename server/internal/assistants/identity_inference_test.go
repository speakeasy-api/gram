package assistants

import (
	"testing"

	"github.com/google/uuid"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestAssistantInferenceGrantRequiresActorAndOwnerAuthority(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		actor, owner bool
	}{
		{"neither", false, false}, {"actor_only", true, false}, {"owner_only", false, true}, {"both", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, err := assistantsInfra.CloneTestDatabase(t, "inference_grant_"+tc.name)
			require.NoError(t, err)
			project := newProvisioningProject(t, db, "inference-grant-"+tc.name)
			core := newProvisioningCore(t, db)
			legacy := createLegacyAssistant(t, db, project, "Inference grant")
			grant := authz.NewGrant(authz.ScopeInferenceExecute, authz.WildcardResource)
			grant.Selector[authz.SelectorKeyProjectID] = project.String()
			selectors, err := grant.Selector.MarshalJSON()
			require.NoError(t, err)
			for user, allowed := range map[string]bool{"user-1": tc.owner, "user-2": tc.actor} {
				if !allowed {
					continue
				}
				_, err = accessrepo.New(db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{OrganizationID: "org-test", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, user), Scope: string(grant.Scope), Selectors: selectors})
				require.NoError(t, err)
			}
			record, err := core.UpgradeAssistantIdentity(t.Context(), assistantidentity.ProvisionParams{OrganizationID: "org-test", ProjectID: project, AssistantID: legacy.ID, ActorUserID: "user-2", AgentID: uuid.Nil, AgentName: ""})
			require.NoError(t, err)
			require.NotNil(t, record.AgentID)
			rows, err := agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: uuid.MustParse(*record.AgentID)})
			require.NoError(t, err)
			var policy []authz.Grant
			for _, row := range rows {
				selector, err := authz.SelectorFromRow(row.Selectors)
				require.NoError(t, err)
				policy = append(policy, authz.NewGrantWithSelector(authz.Scope(row.Scope), selector))
			}
			allowed, err := authz.GrantsAuthorize(policy, authz.InferenceExecuteCheck(project.String()))
			require.NoError(t, err)
			require.Equal(t, tc.actor && tc.owner, allowed)
			allowed, err = authz.GrantsAuthorize(policy, authz.InferenceExecuteCheck(uuid.NewString()))
			require.NoError(t, err)
			require.False(t, allowed, "never grant inference outside the assistant project")
		})
	}
}
