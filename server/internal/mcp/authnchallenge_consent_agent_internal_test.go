package mcp

import (
	"testing"

	"github.com/google/uuid"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestAgentAuthorizationTargetRejectsMetaEndpoint(t *testing.T) {
	t.Parallel()

	endpoint := &ResolvedMcpEndpoint{
		MetaMcpServerID:     uuid.NullUUID{UUID: uuid.New(), Valid: true},
		OrganizationID:      "org-placeholder",
		ProjectID:           uuid.New(),
		UserSessionIssuerID: uuid.New(),
	}

	target, ok := agentAuthorizationTarget(endpoint)
	require.False(t, ok)
	require.Nil(t, target)
}

func TestConsentAgentCandidateOwnershipOrAuthorize(t *testing.T) {
	t.Parallel()
	agent := agentsrepo.Agent{ID: uuid.New(), OrganizationID: "org-a", OwnerUserID: "owner"}
	target := AgentAuthorizationTarget{OrganizationID: "org-a"}
	require.True(t, consentAgentCandidateEligible(consentHumanAuthorization{userID: "owner"}, agent, target))
	require.False(t, consentAgentCandidateEligible(consentHumanAuthorization{userID: "other"}, agent, target))
	require.True(t, consentAgentCandidateEligible(consentHumanAuthorization{userID: "other", grants: []authz.Grant{authz.NewGrant(authz.ScopeAgentAuthorize, agent.ID.String())}}, agent, target))
	require.False(t, consentAgentCandidateEligible(consentHumanAuthorization{userID: "owner"}, agent, AgentAuthorizationTarget{OrganizationID: "org-b"}))
}
