package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agents"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AgentAuthorizationTarget is the fixed requested policy R for an agent MCP
// authorization. The scope is always mcp:connect; these fields bind that one
// scope to the exact challenged endpoint and tenant.
type AgentAuthorizationTarget struct {
	Scope               authz.Scope `json:"scope"`
	OrganizationID      string      `json:"organization_id"`
	ProjectID           uuid.UUID   `json:"project_id"`
	UserSessionIssuerID uuid.UUID   `json:"user_session_issuer_id"`
	MCPResourceID       uuid.UUID   `json:"mcp_resource_id"`
}

// AgentAuthorizationResult records the selected actor and immutable human
// authorizer after all final checks succeed. AIM-197 consumes this handoff to
// mint the existing user-session shape for an agent subject.
type AgentAuthorizationResult struct {
	AgentID          uuid.UUID                `json:"agent_id"`
	AuthorizerUserID string                   `json:"authorizer_user_id"`
	Target           AgentAuthorizationTarget `json:"target"`
}

type consentAgentOption struct {
	ID   string
	Name string
}

type consentHumanAuthorization struct {
	userID string
	grants []authz.Grant
}

func agentAuthorizationTarget(endpoint *ResolvedMcpEndpoint) (*AgentAuthorizationTarget, bool) {
	// Meta-MCP runtime authorization is evaluated against each member MCP
	// resource, not the gateway ID. Keep agent selection disabled until the
	// target can represent and revalidate that member set.
	if endpoint.MetaMcpServerID.Valid {
		return nil, false
	}

	var resourceID uuid.UUID
	switch {
	case endpoint.McpServerID.Valid:
		resourceID = endpoint.McpServerID.UUID
	case endpoint.ToolsetID.Valid:
		resourceID = endpoint.ToolsetID.UUID
	default:
		return nil, false
	}
	if resourceID == uuid.Nil || endpoint.ProjectID == uuid.Nil || endpoint.UserSessionIssuerID == uuid.Nil || endpoint.OrganizationID == "" {
		return nil, false
	}
	return &AgentAuthorizationTarget{
		Scope:               authz.ScopeMCPConnect,
		OrganizationID:      endpoint.OrganizationID,
		ProjectID:           endpoint.ProjectID,
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		MCPResourceID:       resourceID,
	}, true
}

func (t AgentAuthorizationTarget) matches(endpoint *ResolvedMcpEndpoint) bool {
	current, ok := agentAuthorizationTarget(endpoint)
	return ok && t == *current
}

func (t AgentAuthorizationTarget) connectCheck() authz.Check {
	return authz.MCPCheck(t.Scope, t.MCPResourceID.String(), t.ProjectID.String())
}

func (s *Service) agentAuthorizationRollout(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint) (bool, string) {
	organization, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, endpoint.OrganizationID)
	if err != nil {
		logger.WarnContext(ctx, "agent authorization rollout organization unavailable")
		return false, ""
	}
	groups := feature.OrgProjectGroups(organization.Slug, "")
	for _, flag := range []feature.Flag{feature.FlagAgentManagementM1, feature.FlagAgentMCPAuthorizationM2} {
		evaluation, err := feature.EvaluateFlag(ctx, s.features, flag, endpoint.OrganizationID, groups)
		if err != nil || evaluation != feature.EvaluationEnabled {
			if err != nil {
				logger.WarnContext(ctx, "agent authorization rollout evaluation unavailable")
			}
			return false, ""
		}
	}
	setupURL, err := url.JoinPath(s.siteURL.String(), organization.Slug, "agent-management")
	if err != nil {
		logger.WarnContext(ctx, "agent setup URL unavailable")
		return false, ""
	}
	return true, setupURL
}

func (s *Service) loadConsentHuman(ctx context.Context, state AuthnChallengeState, target AgentAuthorizationTarget) (consentHumanAuthorization, error) {
	if state.Subject == nil || state.Subject.Kind != urn.SessionSubjectKindUser || state.Subject.ID == "" || state.AuthorizerUserID == "" || state.AuthorizerUserID != state.Subject.ID {
		return consentHumanAuthorization{}, errors.New("agent authorization requires an authenticated human")
	}
	if state.AuthorizerImpersonated == nil {
		return consentHumanAuthorization{}, errors.New("agent authorizer provenance is unavailable")
	}
	if *state.AuthorizerImpersonated {
		return consentHumanAuthorization{}, errors.New("impersonated support sessions cannot authorize agents")
	}

	active, err := orgrepo.New(s.db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{
		UserID:         state.AuthorizerUserID,
		OrganizationID: target.OrganizationID,
	})
	if err != nil {
		return consentHumanAuthorization{}, fmt.Errorf("check authorizer membership: %w", err)
	}
	if !active {
		return consentHumanAuthorization{}, errors.New("agent authorizer is not an active organization member")
	}

	principals, err := authz.ResolveUserPrincipals(ctx, s.db, target.OrganizationID, state.AuthorizerUserID)
	if err != nil {
		return consentHumanAuthorization{}, fmt.Errorf("resolve authorizer policy principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, s.db, target.OrganizationID, principals)
	if err != nil {
		return consentHumanAuthorization{}, fmt.Errorf("load authorizer policy: %w", err)
	}
	return consentHumanAuthorization{userID: state.AuthorizerUserID, grants: grants}, nil
}

func (s *Service) eligibleConsentAgents(ctx context.Context, state AuthnChallengeState, endpoint *ResolvedMcpEndpoint) ([]consentAgentOption, error) {
	target := state.AgentAuthorizationTarget
	if target == nil || !target.matches(endpoint) {
		return nil, errors.New("agent authorization target does not match endpoint")
	}
	human, err := s.loadConsentHuman(ctx, state, *target)
	if err != nil {
		return nil, err
	}
	candidates, err := agentsrepo.New(s.db).ListActiveAgentsForAuthorization(ctx, target.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list agent authorization candidates: %w", err)
	}

	agentIDs := make([]uuid.UUID, 0, len(candidates))
	for _, candidate := range candidates {
		agentIDs = append(agentIDs, candidate.ID)
	}
	policies, err := authz.LoadKnownAgentPolicies(ctx, s.db, target.OrganizationID, agentIDs)
	if err != nil {
		return nil, fmt.Errorf("load candidate agent policies: %w", err)
	}

	result := make([]consentAgentOption, 0, len(candidates))
	for _, candidate := range candidates {
		if !consentAgentCandidateEligible(human, candidate, *target) {
			continue
		}
		if authz.GrantsSatisfy(policies[candidate.ID], target.connectCheck()) {
			result = append(result, consentAgentOption{ID: candidate.ID.String(), Name: candidate.Name})
		}
	}
	return result, nil
}

func (s *Service) authorizeConsentAgent(ctx context.Context, state AuthnChallengeState, endpoint *ResolvedMcpEndpoint, rawAgentID string) (*AgentAuthorizationResult, error) {
	target := state.AgentAuthorizationTarget
	if target == nil || !target.matches(endpoint) {
		return nil, errors.New("agent authorization target does not match endpoint")
	}
	human, err := s.loadConsentHuman(ctx, state, *target)
	if err != nil {
		return nil, err
	}
	agentID, err := uuid.Parse(rawAgentID)
	if err != nil {
		return nil, errors.New("selected agent is not eligible")
	}
	agent, err := agentsrepo.New(s.db).GetAgentByID(ctx, agentsrepo.GetAgentByIDParams{
		OrganizationID: target.OrganizationID,
		ID:             agentID,
	})
	if err != nil {
		return nil, errors.New("selected agent is not eligible")
	}
	eligible, err := s.consentAgentEligible(ctx, human, agent, *target)
	if err != nil {
		return nil, err
	}
	if !eligible {
		return nil, errors.New("selected agent is not eligible")
	}
	return &AgentAuthorizationResult{AgentID: agent.ID, AuthorizerUserID: human.userID, Target: *target}, nil
}

func (s *Service) consentAgentEligible(ctx context.Context, human consentHumanAuthorization, agent agentsrepo.Agent, target AgentAuthorizationTarget) (bool, error) {
	if !consentAgentCandidateEligible(human, agent, target) {
		return false, nil
	}
	ownerActive, err := orgrepo.New(s.db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{
		UserID:         agent.OwnerUserID,
		OrganizationID: target.OrganizationID,
	})
	if err != nil {
		return false, fmt.Errorf("check agent owner membership: %w", err)
	}
	if !ownerActive {
		return false, nil
	}

	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	agentPolicy, err := authz.LoadAgentPolicy(ctx, s.db, target.OrganizationID, agentPrincipal)
	if err != nil {
		return false, fmt.Errorf("load direct agent policy: %w", err)
	}
	check := target.connectCheck()
	if !authz.GrantsSatisfy(agentPolicy, check) {
		return false, nil
	}
	ownerPrincipals, err := authz.ResolveUserPrincipals(ctx, s.db, target.OrganizationID, agent.OwnerUserID)
	if err != nil {
		return false, fmt.Errorf("resolve agent owner policy principals: %w", err)
	}
	ownerPolicy, err := authz.LoadGrants(ctx, s.db, target.OrganizationID, ownerPrincipals)
	if err != nil {
		return false, fmt.Errorf("load agent owner policy: %w", err)
	}
	return authz.GrantsSatisfy(ownerPolicy, check), nil
}

func consentAgentCandidateEligible(human consentHumanAuthorization, agent agentsrepo.Agent, target AgentAuthorizationTarget) bool {
	if agent.OrganizationID != target.OrganizationID || agents.DeriveLifecycle(agent) != agents.LifecycleActive || agent.OwnerReassignmentRequiredAt.Valid {
		return false
	}
	return agent.OwnerUserID == human.userID || authz.GrantsSatisfy(human.grants, authz.Check{
		Scope:        authz.ScopeAgentAuthorize,
		ResourceKind: authz.ResourceKindAgent,
		ResourceID:   agent.ID.String(),
		Dimensions:   nil,
	})
}
