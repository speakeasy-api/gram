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
// directory ceiling. Live admission still intersects it with the agent owner's
// and invoking user's permissions. Ordinary assistants never receive this grant.
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
	selector := authz.NewSelector(authz.ScopeOrgRead, organizationID)
	for _, row := range grants {
		if row.Scope != string(authz.ScopeOrgRead) {
			continue
		}
		// Runtime policy loading drops selectors that fail validation, so such a
		// grant is no ceiling and the canonical one is still created.
		stored, err := authz.SelectorFromRow(row.Selectors)
		if err != nil || authz.ValidateSelector(authz.ScopeOrgRead, stored) != nil {
			continue
		}
		if authz.GrantsContainSelectorFor(actorUserID, []authz.Grant{authz.NewGrantWithSelector(authz.ScopeOrgRead, stored)}, authz.ScopeOrgRead, selector) {
			return nil
		}
	}
	encoded, err := json.Marshal(selector)
	if err != nil {
		return fmt.Errorf("encode org read ceiling: %w", err)
	}
	row, err := agents.CreateAgentPolicyGrant(ctx, agentrepo.CreateAgentPolicyGrantParams{OrganizationID: organizationID, AgentID: binding.OriginalAgentID, Scope: string(authz.ScopeOrgRead), Selectors: encoded})
	if err != nil {
		return fmt.Errorf("grant managed assistant org read: %w", err)
	}
	if err := s.audit.LogAgentPolicyGrant(ctx, tx, audit.LogAgentPolicyGrantEvent{
		OrganizationID: organizationID, AgentURN: urn.NewAgentIdentity(binding.OriginalAgentID.String()), Actor: urn.NewPrincipal(urn.PrincipalTypeUser, actorUserID), ActorDisplayName: nil,
		Action: audit.ActionAgentPolicyGrantCreate, Name: agent.Name, Before: nil,
		After: &audit.AgentPolicyGrantSnapshot{ID: row.ID, Scope: row.Scope, Effect: "allow", Selector: selector},
	}); err != nil {
		return fmt.Errorf("audit managed assistant org read: %w", err)
	}
	return nil
}
