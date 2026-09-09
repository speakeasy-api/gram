package audit

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type stubAgentManagementTargetResolver struct {
	agent  agentrepo.Agent
	err    error
	params agentrepo.GetAgentByIDParams
}

func (s *stubAgentManagementTargetResolver) GetAgentByID(_ context.Context, params agentrepo.GetAgentByIDParams) (agentrepo.Agent, error) {
	s.params = params
	return s.agent, s.err
}

func TestNewAgentManagementAttributionUsesTrustedCanonicalActor(t *testing.T) {
	t.Parallel()

	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: "org_123",
		UserID:               "user_actor",
	}
	ctx := contextvalues.WithValidatedGramSession(t.Context(), authCtx, false)

	// Mutating attribution-only public fields cannot change the trusted actor.
	validated, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	validated.UserID = "user_not_actor"

	agentID := uuid.MustParse("018f8d7b-58d7-7cc4-bb16-9f8c6b99a001")
	resolver := &stubAgentManagementTargetResolver{agent: agentrepo.Agent{
		ID: agentID, OrganizationID: "org_123", OwnerUserID: "user_current_owner",
	}}
	attribution, err := NewAgentManagementAttribution(ctx, resolver, agentID, AgentManagementOwnerChange{
		FormerOwnerUserID:      "user_former_owner",
		ReplacementOwnerUserID: "user_replacement_owner",
	})
	require.NoError(t, err)
	require.Equal(t, "org_123", attribution.OrganizationID)
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeUser, "user_actor"), attribution.Actor)
	require.Equal(t, agentID, attribution.AffectedAgentID)
	require.Equal(t, "user_current_owner", attribution.CurrentOwnerUserID)
	require.Equal(t, "user_former_owner", attribution.FormerOwnerUserID)
	require.Equal(t, "user_replacement_owner", attribution.ReplacementOwnerUserID)
	require.Equal(t, agentrepo.GetAgentByIDParams{OrganizationID: "org_123", ID: agentID}, resolver.params)
}

func TestNewAgentManagementAttributionDoesNotInferActor(t *testing.T) {
	t.Parallel()

	agentID := uuid.MustParse("018f8d7b-58d7-7cc4-bb16-9f8c6b99a001")
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_123",
		UserID:               "user_owner",
		APIKeyID:             "key_123",
	})
	resolver := &stubAgentManagementTargetResolver{agent: agentrepo.Agent{
		ID: agentID, OrganizationID: "org_123", OwnerUserID: "user_owner",
	}}

	_, err := NewAgentManagementAttribution(ctx, resolver, agentID, AgentManagementOwnerChange{})
	require.ErrorIs(t, err, ErrInvalidAgentManagementAttribution)
}

func TestNewAgentManagementAttributionFailsClosedForInvalidTenantBoundTarget(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_authenticated",
		UserID:               "user_actor",
	}, false)
	foreignAgentID := uuid.MustParse("018f8d7b-58d7-7cc4-bb16-9f8c6b99a002")

	tests := map[string]*stubAgentManagementTargetResolver{
		"cross tenant":  {agent: agentrepo.Agent{ID: foreignAgentID, OrganizationID: "org_foreign", OwnerUserID: "user_foreign"}},
		"missing agent": {err: errors.New("not found")},
		"missing owner": {agent: agentrepo.Agent{ID: foreignAgentID, OrganizationID: "org_authenticated"}},
	}
	for name, resolver := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewAgentManagementAttribution(ctx, resolver, foreignAgentID, AgentManagementOwnerChange{})
			require.ErrorIs(t, err, ErrInvalidAgentManagementAttribution)
			require.Equal(t, ErrInvalidAgentManagementAttribution.Error(), err.Error())
			require.NotContains(t, err.Error(), "org_foreign")
			require.NotContains(t, err.Error(), "user_foreign")
		})
	}
}
