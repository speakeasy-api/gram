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
	require.True(t, consentAgentCandidateEligible("owner", authz.NewAgentAccess("owner", nil), agent, target))
	require.False(t, consentAgentCandidateEligible("other", authz.NewAgentAccess("other", nil), agent, target))
	require.True(t, consentAgentCandidateEligible("other", authz.NewAgentAccess("other", []authz.Grant{authz.NewGrant(authz.ScopeAgentAuthorize, agent.ID.String())}), agent, target))
	require.False(t, consentAgentCandidateEligible("owner", authz.NewAgentAccess("owner", nil), agent, AgentAuthorizationTarget{OrganizationID: "org-b"}))
}

func TestConsentAgentCandidateHonorsOwnerSelector(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		owner   string
		allowed bool
	}{
		{owner: "owner", allowed: true},
		{owner: "different-owner"},
		{owner: "self"},
		{owner: "*", allowed: true},
	} {
		t.Run(tc.owner, func(t *testing.T) {
			t.Parallel()
			agent := agentsrepo.Agent{ID: uuid.New(), OrganizationID: "org-a", OwnerUserID: "owner"}
			human := consentHumanAuthorization{userID: "caller", grants: []authz.Grant{
				authz.NewGrantWithSelector(authz.ScopeAgentAuthorize, authz.AgentOwnerSelector(authz.ScopeAgentAuthorize, tc.owner)),
			}}
			access := authz.NewAgentAccess(human.userID, human.grants)
			require.Equal(t, tc.allowed, consentAgentCandidateEligible(human.userID, access, agent, AgentAuthorizationTarget{OrganizationID: "org-a"}))
		})
	}
}
