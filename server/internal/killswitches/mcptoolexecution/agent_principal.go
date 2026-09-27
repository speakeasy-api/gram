package mcptoolexecution

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AgentPrincipalAdapter resolves concrete agents, never their owners or authorizers.
type AgentPrincipalAdapter struct{ db *pgxpool.Pool }

func NewAgentPrincipalAdapter(db *pgxpool.Pool) *AgentPrincipalAdapter {
	return &AgentPrincipalAdapter{db: db}
}

func (a *AgentPrincipalAdapter) Kind() killswitches.PrincipalKind { return PrincipalKindAgent }

func (a *AgentPrincipalAdapter) Canonicalize(_ killswitches.OrganizationID, input string) (killswitches.CanonicalizationResult[killswitches.PrincipalKey], error) {
	id, err := uuid.Parse(strings.TrimSpace(input))
	if err != nil || id == uuid.Nil {
		return killswitches.UnsupportedCanonicalizationResult[killswitches.PrincipalKey](), nil
	}
	result, err := killswitches.NewCanonicalizationResult(killswitches.PrincipalKey(id.String()))
	if err != nil {
		return result, fmt.Errorf("canonicalize agent principal: %w", err)
	}
	return result, nil
}

func (a *AgentPrincipalAdapter) ValidateCurrentOrganization(ctx context.Context, organizationID killswitches.OrganizationID, key killswitches.PrincipalKey) (bool, error) {
	id, err := uuid.Parse(string(key))
	if err != nil || id == uuid.Nil || id.String() != string(key) || organizationID == "" {
		return false, nil
	}
	agent, err := agents.ResolvePrincipal(ctx, a.db, string(organizationID), urn.NewPrincipal(urn.PrincipalTypeAgent, id.String()))
	if errors.Is(err, agents.ErrPrincipalNotFound) || errors.Is(err, agents.ErrPrincipalInvalid) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("validate agent principal: %w", err)
	}
	return !agent.SuspendedAt.Valid && !agent.RevokedAt.Valid && !agent.OwnerReassignmentRequiredAt.Valid, nil
}

func (a *AgentPrincipalAdapter) DeriveCandidates(ctx context.Context, organizationID killswitches.OrganizationID, source any) (killswitches.PrincipalCandidateResult, error) {
	identity, ok := source.(mcpidentity.Identity)
	if !ok || organizationID == "" {
		return killswitches.PrincipalCandidateResult{}, errors.New("invalid agent provenance or organization")
	}
	switch identity.Kind() {
	case mcpidentity.KindAgent:
	case mcpidentity.KindUserSession, mcpidentity.KindAPIKey, mcpidentity.KindAnonymous, mcpidentity.KindAssistant, mcpidentity.KindChatSession:
		return killswitches.UnsupportedPrincipalCandidateResult(), nil
	default:
		return killswitches.PrincipalCandidateResult{}, errors.New("unknown identity provenance kind")
	}
	key := killswitches.PrincipalKey(identity.AgentID())
	valid, err := a.ValidateCurrentOrganization(ctx, organizationID, key)
	if err != nil {
		return killswitches.PrincipalCandidateResult{}, err
	}
	if !valid {
		return killswitches.PrincipalCandidateResult{}, errors.New("authenticated agent is not active in the organization")
	}
	result, err := killswitches.NewPrincipalCandidateResult([]killswitches.PrincipalCandidate{{Kind: PrincipalKindAgent, Key: key}})
	if err != nil {
		return result, fmt.Errorf("build agent principal candidates: %w", err)
	}
	return result, nil
}

// authenticatedPrincipalAdapter dispatches by trusted provenance. A request
// contributes exactly its actor's namespace, not credential-owner user rules.
type authenticatedPrincipalAdapter struct {
	killswitches.PrincipalAdapter
	agent killswitches.PrincipalAdapter
}

func (a *authenticatedPrincipalAdapter) DeriveCandidates(ctx context.Context, org killswitches.OrganizationID, source any) (killswitches.PrincipalCandidateResult, error) {
	if identity, ok := source.(mcpidentity.Identity); ok && identity.Kind() == mcpidentity.KindAgent {
		result, err := a.agent.DeriveCandidates(ctx, org, source)
		if err != nil {
			return result, fmt.Errorf("derive agent candidates: %w", err)
		}
		return result, nil
	}
	result, err := a.PrincipalAdapter.DeriveCandidates(ctx, org, source)
	if err != nil {
		return result, fmt.Errorf("derive user candidates: %w", err)
	}
	return result, nil
}

func registeredPrincipalAdapter(registry *killswitches.Registry) (killswitches.PrincipalAdapter, error) {
	user, ok := registry.PrincipalAdapter(PrincipalKindUser)
	if !ok {
		return nil, errors.New("authenticated user principal adapter is not registered")
	}
	agent, ok := registry.PrincipalAdapter(PrincipalKindAgent)
	if !ok {
		return nil, errors.New("authenticated agent principal adapter is not registered")
	}
	return &authenticatedPrincipalAdapter{PrincipalAdapter: user, agent: agent}, nil
}
