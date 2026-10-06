package assistantidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agentownership"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Provision points an assistant at a new dedicated agent and binds its
// existing root triggers. An assistant that already has a binding only gets
// its unbound root triggers bound. The caller owns the
// transaction; a failed call must roll it back.
//
// The agent is owned by the assistant's creator (the actor when the creator is
// unknown) and starts with the project-wide ceiling from initialGrants. Users
// refine that policy on the agent afterwards; it is never re-synchronised.
func (s *Service) Provision(ctx context.Context, tx pgx.Tx, p ProvisionParams) error {
	if p.ActorUserID == "" || p.ActorUserID == urn.AllUsersPrincipalID {
		return ErrActorIneligible
	}
	q := repo.New(tx)
	assistant, err := q.LockAssistant(ctx, repo.LockAssistantParams{ProjectID: p.ProjectID, AssistantID: p.AssistantID})
	if err != nil {
		return resourceError("lock assistant", err)
	}
	if assistant.OrganizationID != p.OrganizationID {
		return fmt.Errorf("lock assistant: %w", ErrNotFound)
	}

	_, err = q.GetAssistantBinding(ctx, repo.GetAssistantBindingParams{ProjectID: p.ProjectID, AssistantID: p.AssistantID})
	switch {
	case err == nil:
		return s.bindExistingRoots(ctx, tx, p)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read assistant binding: %w", err)
	}

	owner := conv.FromPGTextOrEmpty[string](assistant.CreatedByUserID)
	if owner == "" {
		owner = p.ActorUserID
	}
	agents := agentrepo.New(tx)
	agent, err := agents.CreateAgent(ctx, agentrepo.CreateAgentParams{
		OrganizationID: p.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: p.ProjectID, Valid: true},
		OwnerUserID:    owner,
		Name:           "Assistant " + p.AssistantID.String(),
	})
	if err != nil {
		return fmt.Errorf("create dedicated assistant agent: %w", err)
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, p.ActorUserID)
	agentURN := urn.NewAgentIdentity(agent.ID.String())
	if err := s.audit.LogAgent(ctx, tx, audit.LogAgentEvent{
		OrganizationID: p.OrganizationID, AgentURN: agentURN, Actor: actor, ActorDisplayName: nil,
		Action: audit.ActionAgentCreate, Name: agent.Name, Before: nil, After: agentownership.AgentAuditSnapshot(agent),
	}); err != nil {
		return fmt.Errorf("audit dedicated assistant agent: %w", err)
	}

	for _, grant := range initialGrants(p.ProjectID, p.AssistantID) {
		selector, err := json.Marshal(grant.Selector)
		if err != nil {
			return fmt.Errorf("encode assistant agent grant: %w", err)
		}
		row, err := agents.CreateAgentPolicyGrant(ctx, agentrepo.CreateAgentPolicyGrantParams{
			OrganizationID: p.OrganizationID, AgentID: agent.ID, Scope: string(grant.Scope), Selectors: selector,
		})
		if err != nil {
			return fmt.Errorf("write assistant agent grant: %w", err)
		}
		if err := s.audit.LogAgentPolicyGrant(ctx, tx, audit.LogAgentPolicyGrantEvent{
			OrganizationID: p.OrganizationID, AgentURN: agentURN, Actor: actor, ActorDisplayName: nil,
			Action: audit.ActionAgentPolicyGrantCreate, Name: agent.Name, Before: nil,
			After: &audit.AgentPolicyGrantSnapshot{ID: row.ID, Scope: row.Scope, Effect: "allow", Selector: grant.Selector},
		}); err != nil {
			return fmt.Errorf("audit assistant agent grant: %w", err)
		}
	}

	if _, err := q.CreateAssistantBinding(ctx, repo.CreateAssistantBindingParams{
		OrganizationID: p.OrganizationID, ProjectID: p.ProjectID, AssistantID: p.AssistantID, AgentID: agent.ID,
	}); err != nil {
		return fmt.Errorf("create assistant binding: %w", err)
	}
	if err := s.audit.LogAssistantIdentityProvision(ctx, tx, audit.LogAssistantIdentityProvisionEvent{
		OrganizationID: p.OrganizationID, ProjectID: p.ProjectID, Actor: actor, ActorDisplayName: nil, ActorSlug: nil,
		AssistantURN: urn.NewAssistant(p.AssistantID), AssistantName: assistant.Name, AgentURN: agentURN,
	}); err != nil {
		return fmt.Errorf("audit assistant identity provisioning: %w", err)
	}
	return s.bindExistingRoots(ctx, tx, p)
}

// initialGrants is the dedicated agent's starting ceiling: every MCP server
// and skill in the assistant's project, plus administration of the assistant
// itself.
func initialGrants(project, assistant uuid.UUID) []authz.Grant {
	projectMCP := authz.NewSelector(authz.ScopeMCPConnect, authz.WildcardResource)
	projectMCP[authz.SelectorKeyProjectID] = project.String()
	grants := []authz.Grant{
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, projectMCP),
		authz.NewGrantWithSelector(authz.ScopeMCPRead, projectMCP),
		// Skill selectors carry no project dimension; the project ID is the
		// resource the project-wide skill:read check names.
		authz.NewGrant(authz.ScopeSkillRead, project.String()),
	}
	return append(grants, assistantSelfAdminGrants(assistant)...)
}

// assistantSelfAdminGrants lets the agent administer exactly its own
// assistant; assistant:write implies assistant:read.
func assistantSelfAdminGrants(assistant uuid.UUID) []authz.Grant {
	return []authz.Grant{authz.NewGrant(authz.ScopeAssistantWrite, assistant.String())}
}

func (s *Service) bindExistingRoots(ctx context.Context, tx pgx.Tx, p ProvisionParams) error {
	// Locks every root in ID order before any issuer lock is taken.
	roots, err := repo.New(tx).ListAssistantRoots(ctx, repo.ListAssistantRootsParams{ProjectID: p.ProjectID, AssistantID: p.AssistantID.String()})
	if err != nil {
		return fmt.Errorf("list assistant root triggers: %w", err)
	}
	for _, root := range roots {
		if err := s.bindRoot(ctx, tx, p.ProjectID, root, false, auditActor{principal: urn.NewPrincipal(urn.PrincipalTypeUser, p.ActorUserID), displayName: nil}); err != nil {
			return err
		}
	}
	return nil
}

func resourceError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", operation, ErrNotFound)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
