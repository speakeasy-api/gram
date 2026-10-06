package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func agentGrantScopes(t *testing.T, agents *agentrepo.Queries, agentID uuid.UUID) []string {
	t.Helper()
	rows, err := agents.ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: agentID})
	require.NoError(t, err)
	scopes := make([]string, 0, len(rows))
	for _, row := range rows {
		scopes = append(scopes, row.Scope)
	}
	return scopes
}

func TestUpgradeAssistantIdentityWithExistingAgent(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_existing_agent")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-existing-agent")
	core := newProvisioningCore(t, db)
	agents := agentrepo.New(db)
	existing, err := agents.CreateAgent(t.Context(), agentrepo.CreateAgentParams{OrganizationID: "org-test", OwnerUserID: "user-1", ProjectID: uuid.NullUUID{UUID: project, Valid: true}, Name: "Existing agent"})
	require.NoError(t, err)

	legacy := createLegacyAssistant(t, db, project, "Uses an existing agent")
	params := assistantidentity.ProvisionParams{OrganizationID: "org-test", ProjectID: project, AssistantID: legacy.ID, ActorUserID: "user-1", AgentID: existing.ID, AgentName: ""}
	record, err := core.UpgradeAssistantIdentity(t.Context(), params)
	require.NoError(t, err)
	require.Equal(t, string(assistantidentity.Active), record.IdentityState)
	require.Equal(t, existing.ID.String(), *record.AgentID)
	require.Equal(t, []string{string(authz.ScopeAssistantWrite)}, agentGrantScopes(t, agents, existing.ID), "an existing agent keeps its policy and only gains administration of its assistant")
	_, err = core.UpgradeAssistantIdentity(t.Context(), params)
	require.NoError(t, err, "repeating the upgrade with the same agent is safe")

	other := createLegacyAssistant(t, db, project, "Wants the same agent")
	_, err = core.UpgradeAssistantIdentity(t.Context(), assistantidentity.ProvisionParams{OrganizationID: "org-test", ProjectID: project, AssistantID: other.ID, ActorUserID: "user-1", AgentID: existing.ID, AgentName: ""})
	require.Error(t, err, "an agent backs at most one assistant")

	foreignProject := newProvisioningProject(t, db, "identity-existing-agent-other")
	foreign, err := agents.CreateAgent(t.Context(), agentrepo.CreateAgentParams{OrganizationID: "org-test", OwnerUserID: "user-1", ProjectID: uuid.NullUUID{UUID: foreignProject, Valid: true}, Name: "Foreign agent"})
	require.NoError(t, err)
	_, err = core.UpgradeAssistantIdentity(t.Context(), assistantidentity.ProvisionParams{OrganizationID: "org-test", ProjectID: project, AssistantID: other.ID, ActorUserID: "user-1", AgentID: foreign.ID, AgentName: ""})
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)

	require.NoError(t, core.DeleteAssistant(t.Context(), project, legacy.ID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), nil))
	kept, err := agents.GetAgentByID(t.Context(), agentrepo.GetAgentByIDParams{OrganizationID: "org-test", ID: existing.ID})
	require.NoError(t, err)
	require.False(t, kept.RevokedAt.Valid, "deleting the assistant leaves its agent in place")
	_, err = core.UpgradeAssistantIdentity(t.Context(), assistantidentity.ProvisionParams{OrganizationID: "org-test", ProjectID: project, AssistantID: other.ID, ActorUserID: "user-1", AgentID: existing.ID, AgentName: ""})
	require.NoError(t, err, "a released agent can back another assistant")
}

func TestUpgradeAssistantIdentityNamesNewAgent(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_named_agent")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-named-agent")
	core := newProvisioningCore(t, db)
	legacy := createLegacyAssistant(t, db, project, "Named agent")
	record, err := core.UpgradeAssistantIdentity(t.Context(), assistantidentity.ProvisionParams{OrganizationID: "org-test", ProjectID: project, AssistantID: legacy.ID, ActorUserID: "user-1", AgentID: uuid.Nil, AgentName: "Support agent"})
	require.NoError(t, err)
	agent, err := agentrepo.New(db).GetAgentByID(t.Context(), agentrepo.GetAgentByIDParams{OrganizationID: "org-test", ID: uuid.MustParse(*record.AgentID)})
	require.NoError(t, err)
	require.Equal(t, "Support agent", agent.Name)
}

func TestUpgradeAssistantIdentityRequiresAuthorityOverExistingAgent(t *testing.T) {
	t.Parallel()
	svc, ctx, project, db := newRBACServiceWithConn(t, "identity_agent_authority")
	legacy := createLegacyAssistant(t, db, project, "Agent authority")
	agent, err := agentrepo.New(db).CreateAgent(t.Context(), agentrepo.CreateAgentParams{OrganizationID: "org-test", OwnerUserID: "user-2", ProjectID: uuid.NullUUID{UUID: project, Valid: true}, Name: "Someone else's agent"})
	require.NoError(t, err)
	payload := &gen.UpgradeAssistantIdentityPayload{ID: legacy.ID.String(), AgentID: new(agent.ID.String()), AgentName: nil, SessionToken: nil, ProjectSlugInput: nil}
	projectWrite := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, project.String()))

	_, err = svc.UpgradeAssistantIdentity(projectWrite, payload)
	var denied *oops.ShareableError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, oops.CodeForbidden, denied.Code)
	missing := &gen.UpgradeAssistantIdentityPayload{ID: legacy.ID.String(), AgentID: new(uuid.NewString()), AgentName: nil, SessionToken: nil, ProjectSlugInput: nil}
	_, err = svc.UpgradeAssistantIdentity(projectWrite, missing)
	require.ErrorAs(t, err, &denied)
	require.Equal(t, oops.CodeForbidden, denied.Code, "a missing agent is indistinguishable from an unauthorized one")

	selector, err := authz.NewSelector(authz.ScopeAgentAuthorize, agent.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{OrganizationID: "org-test", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, "user-test"), Scope: string(authz.ScopeAgentAuthorize), Selectors: selector})
	require.NoError(t, err)
	svc.features = identityFlags(false)
	_, err = svc.UpgradeAssistantIdentity(projectWrite, payload)
	require.ErrorAs(t, err, &denied)
	require.Equal(t, oops.CodeForbidden, denied.Code, "selecting an existing agent is gated on the identity rollout")
	require.Empty(t, agentGrantScopes(t, agentrepo.New(db), agent.ID))

	svc.features = identityFlags(true)
	upgraded, err := svc.UpgradeAssistantIdentity(projectWrite, payload)
	require.NoError(t, err)
	require.Equal(t, agent.ID.String(), *upgraded.AgentID)

	both := &gen.UpgradeAssistantIdentityPayload{ID: legacy.ID.String(), AgentID: new(agent.ID.String()), AgentName: new("Name"), SessionToken: nil, ProjectSlugInput: nil}
	_, err = svc.UpgradeAssistantIdentity(projectWrite, both)
	require.ErrorAs(t, err, &denied)
	require.Equal(t, oops.CodeBadRequest, denied.Code)
}
