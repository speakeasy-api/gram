package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// errManagedOrganizationReadTarget rejects granting the managed ceiling to an
// assistant that is not its project's managed assistant.
var errManagedOrganizationReadTarget = errors.New("managed assistant org access target mismatch")

// ensureManagedOrganizationRead supplies the built-in project assistant's org
// directory ceiling and its own project's read ceiling, which the Platform
// telemetry tools require. Live admission still intersects them with the agent
// owner's and invoking user's permissions. Ordinary assistants never receive
// these grants.
// An assistant whose agent was deleted has nothing to repair.
func (s *ServiceCore) ensureManagedOrganizationRead(ctx context.Context, tx pgx.Tx, organizationID string, projectID, assistantID uuid.UUID, actorUserID string) error {
	managed, err := assistantrepo.New(tx).GetManagedAssistantByProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("read managed assistant for org access: %w", err)
	}
	if managed.ID != assistantID || managed.OrganizationID != organizationID {
		return errManagedOrganizationReadTarget
	}
	q := identityrepo.New(tx)
	if _, err := q.LockAssistant(ctx, identityrepo.LockAssistantParams{ProjectID: projectID, AssistantID: assistantID}); err != nil {
		return fmt.Errorf("lock managed assistant: %w", err)
	}
	binding, err := q.GetAssistantBinding(ctx, identityrepo.GetAssistantBindingParams{ProjectID: projectID, AssistantID: assistantID})
	if err != nil {
		return fmt.Errorf("get managed assistant agent: %w", err)
	}
	agents := agentrepo.New(tx)
	agent, err := agents.GetAgentByIDForUpdate(ctx, agentrepo.GetAgentByIDForUpdateParams{OrganizationID: organizationID, ID: binding.OriginalAgentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock managed assistant agent: %w", err)
	}
	grants, err := agents.ListAgentPolicyGrants(ctx, agentrepo.ListAgentPolicyGrantsParams{OrganizationID: organizationID, AgentID: binding.OriginalAgentID})
	if err != nil {
		return fmt.Errorf("read managed assistant policy: %w", err)
	}
	ceilings := []authz.Grant{
		authz.NewGrant(authz.ScopeOrgRead, organizationID),
		authz.NewGrant(authz.ScopeProjectRead, projectID.String()),
	}
	for _, ceiling := range ceilings {
		if hasManagedCeiling(grants, actorUserID, ceiling) {
			continue
		}
		encoded, err := json.Marshal(ceiling.Selector)
		if err != nil {
			return fmt.Errorf("encode %s ceiling: %w", ceiling.Scope, err)
		}
		row, err := agents.EnsurePermanentAgentPolicyGrant(ctx, agentrepo.EnsurePermanentAgentPolicyGrantParams{OrganizationID: organizationID, AgentID: binding.OriginalAgentID, Scope: string(ceiling.Scope), Selectors: encoded})
		if err != nil {
			return fmt.Errorf("grant managed assistant %s: %w", ceiling.Scope, err)
		}
		if err := s.audit.LogAgentPolicyGrant(ctx, tx, audit.LogAgentPolicyGrantEvent{
			OrganizationID: organizationID, AgentURN: urn.NewAgentIdentity(binding.OriginalAgentID.String()), Actor: urn.NewPrincipal(urn.PrincipalTypeUser, actorUserID), ActorDisplayName: nil,
			Action: audit.ActionAgentPolicyGrantCreate, Name: agent.Name, Before: nil,
			After: &audit.AgentPolicyGrantSnapshot{ID: row.ID, Scope: row.Scope, Effect: "allow", Selector: ceiling.Selector},
		}); err != nil {
			return fmt.Errorf("audit managed assistant %s: %w", ceiling.Scope, err)
		}
	}
	return nil
}

// hasManagedCeiling reports whether the agent policy already holds a valid,
// permanent grant that covers ceiling. Runtime policy loading drops selectors
// that fail validation, and a temporary grant lapses, so neither is a ceiling
// and the canonical one is still created.
func hasManagedCeiling(rows []agentrepo.ListAgentPolicyGrantsRow, actorUserID string, ceiling authz.Grant) bool {
	for _, row := range rows {
		if row.Scope != string(ceiling.Scope) || row.ExpiresAt.Valid {
			continue
		}
		stored, err := authz.SelectorFromRow(row.Selectors)
		if err != nil || authz.ValidateSelector(ceiling.Scope, stored) != nil {
			continue
		}
		if authz.GrantsContainSelectorFor(actorUserID, []authz.Grant{authz.NewGrantWithSelector(ceiling.Scope, stored)}, ceiling.Scope, ceiling.Selector) {
			return true
		}
	}
	return false
}
