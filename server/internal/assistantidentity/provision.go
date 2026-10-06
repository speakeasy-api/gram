package assistantidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agentownership"
	"github.com/speakeasy-api/gram/server/internal/agents/lifecycle"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Provision points an assistant at an agent and binds its existing root
// triggers: a new agent, or the existing one p.AgentID names. An assistant
// that already has a binding only gets its unbound root triggers bound. The
// caller owns the transaction; a failed call must roll it back.
//
// A new agent starts with the project-wide ceiling from initialGrants. Users
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

	if p.AgentID != uuid.Nil && p.AgentName != "" {
		return ErrInvalidIdentity
	}
	existing, err := q.GetAssistantBinding(ctx, repo.GetAssistantBindingParams{ProjectID: p.ProjectID, AssistantID: p.AssistantID})
	switch {
	case err == nil:
		if p.AgentName != "" || (p.AgentID != uuid.Nil && p.AgentID != existing.OriginalAgentID) {
			return ErrInvalidIdentity
		}
		return s.bindExistingRoots(ctx, tx, p)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read assistant binding: %w", err)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, p.ActorUserID)
	var agent agentrepo.Agent
	var grants []authz.Grant
	if p.AgentID != uuid.Nil {
		// An existing agent keeps its policy; it only gains administration of
		// the assistant it now backs.
		agent, err = s.selectAgent(ctx, tx, p)
		if err != nil {
			return err
		}
		grants = assistantSelfAdminGrants(p.AssistantID)
	} else {
		agent, err = s.createAgent(ctx, tx, p, conv.FromPGTextOrEmpty[string](assistant.CreatedByUserID), actor)
		if err != nil {
			return err
		}
		grants = initialGrants(p.ProjectID, p.AssistantID)
	}
	agentURN := urn.NewAgentIdentity(agent.ID.String())
	agents := agentrepo.New(tx)
	for _, grant := range grants {
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

// createAgent creates the assistant's agent, owned by the assistant's creator
// (the actor when the creator is unknown).
func (s *Service) createAgent(ctx context.Context, tx pgx.Tx, p ProvisionParams, creator string, actor urn.Principal) (agentrepo.Agent, error) {
	owner := creator
	if owner == "" {
		owner = p.ActorUserID
	}
	name := p.AgentName
	if name == "" {
		name = "Assistant " + p.AssistantID.String()
	}
	agent, err := agentrepo.New(tx).CreateAgent(ctx, agentrepo.CreateAgentParams{
		OrganizationID: p.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: p.ProjectID, Valid: true},
		OwnerUserID:    owner,
		Name:           name,
	})
	if err != nil {
		return agentrepo.Agent{}, fmt.Errorf("create assistant agent: %w", err)
	}
	if err := s.audit.LogAgent(ctx, tx, audit.LogAgentEvent{
		OrganizationID: p.OrganizationID, AgentURN: urn.NewAgentIdentity(agent.ID.String()), Actor: actor, ActorDisplayName: nil,
		Action: audit.ActionAgentCreate, Name: agent.Name, Before: nil, After: agentownership.AgentAuditSnapshot(agent),
	}); err != nil {
		return agentrepo.Agent{}, fmt.Errorf("audit assistant agent: %w", err)
	}
	return agent, nil
}

// selectAgent locks an existing agent of the assistant's project for the
// assistant to point at. Like any other use of an agent, the actor must own it
// or hold agent:authorize on it.
func (s *Service) selectAgent(ctx context.Context, tx pgx.Tx, p ProvisionParams) (agentrepo.Agent, error) {
	agent, err := agentrepo.New(tx).GetAgentByIDForUpdate(ctx, agentrepo.GetAgentByIDForUpdateParams{OrganizationID: p.OrganizationID, ID: p.AgentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return agentrepo.Agent{}, ErrAgentUnauthorized
	}
	if err != nil {
		return agentrepo.Agent{}, fmt.Errorf("lock selected agent: %w", err)
	}
	if agent.OwnerUserID != p.ActorUserID {
		principals, err := authz.ResolveUserPrincipals(ctx, tx, p.OrganizationID, p.ActorUserID)
		if err != nil {
			return agentrepo.Agent{}, fmt.Errorf("resolve actor principals: %w", err)
		}
		grants, err := authz.LoadGrants(ctx, tx, p.OrganizationID, principals)
		if err != nil {
			return agentrepo.Agent{}, fmt.Errorf("load actor grants: %w", err)
		}
		if !authz.GrantsSatisfy(grants, authz.Check{Scope: authz.ScopeAgentAuthorize, ResourceKind: authz.ResourceKindAgent, ResourceID: agent.ID.String(), Dimensions: nil}) {
			return agentrepo.Agent{}, ErrAgentUnauthorized
		}
	}
	if !agent.ProjectID.Valid || agent.ProjectID.UUID != p.ProjectID || lifecycle.Derive(agent) != lifecycle.Active || agent.OwnerReassignmentRequiredAt.Valid {
		return agentrepo.Agent{}, ErrInvalidIdentity
	}
	return agent, nil
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
