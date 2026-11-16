package assistantidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agentownership"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Provision points an assistant at a new dedicated agent and binds its
// existing root triggers. An assistant that already has a binding only gets
// its unbound root triggers bound. The caller owns the transaction; a failed
// call must roll it back.
//
// A new agent starts with the project-wide ceiling from initialGrants. Users
// refine that policy on the agent afterwards; it is never re-synchronised.
func (s *Service) Provision(ctx context.Context, tx pgx.Tx, p ProvisionParams) error {
	actor, err := provisionActor(p)
	if err != nil {
		return err
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
		return s.bindExistingRoots(ctx, tx, p, actor)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read assistant binding: %w", err)
	}

	owner := conv.FromPGTextOrEmpty[string](assistant.CreatedByUserID)
	// Staff never own customer agents: the creator must still be a member.
	delegators := []string{p.ActorUserID, owner}
	if p.Staff != nil {
		active := false
		if owner != "" {
			active, err = orgrepo.New(tx).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{OrganizationID: p.OrganizationID, UserID: owner})
			if err != nil {
				return fmt.Errorf("check assistant creator membership: %w", err)
			}
		}
		if !active {
			return ErrOwnerUnavailable
		}
		delegators = []string{owner}
	} else if owner == "" {
		owner = p.ActorUserID
		delegators[1] = owner
	}
	agent, err := s.createAgent(ctx, tx, p, assistant.Name, owner, actor)
	if err != nil {
		return err
	}
	grants := initialGrants(p.ProjectID, p.AssistantID)
	// A dedicated agent may inherit inference only within this project and
	// only when everyone delegating it already has it: the provisioning user
	// and the owner, or the owner alone when staff provision.
	inherit := true
	for _, user := range delegators {
		allowed, err := canDelegateInference(ctx, tx, p.OrganizationID, p.ProjectID, user)
		if err != nil {
			return err
		}
		if !allowed {
			inherit = false
			break
		}
	}
	if inherit {
		grant := authz.NewGrant(authz.ScopeInferenceExecute, authz.WildcardResource)
		grant.Selector[authz.SelectorKeyProjectID] = p.ProjectID.String()
		grants = append(grants, grant)
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
			OrganizationID: p.OrganizationID, AgentURN: agentURN, Actor: actor.principal, ActorDisplayName: actor.displayName,
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
		OrganizationID: p.OrganizationID, ProjectID: p.ProjectID, Actor: actor.principal, ActorDisplayName: actor.displayName, ActorSlug: nil,
		AssistantURN: urn.NewAssistant(p.AssistantID), AssistantName: assistant.Name, AgentURN: agentURN,
	}); err != nil {
		return fmt.Errorf("audit assistant identity provisioning: %w", err)
	}
	return s.bindExistingRoots(ctx, tx, p, actor)
}

// provisionActor is who a provisioning is attributed to: the staff member, or
// the authenticated user.
func provisionActor(p ProvisionParams) (auditActor, error) {
	if p.Staff != nil {
		if p.ActorUserID != "" || p.Staff.Principal.ID == "" {
			return auditActor{}, ErrActorIneligible
		}
		return auditActor{principal: p.Staff.Principal, displayName: p.Staff.DisplayName}, nil
	}
	if p.ActorUserID == "" || p.ActorUserID == urn.AllUsersPrincipalID {
		return auditActor{}, ErrActorIneligible
	}
	return auditActor{principal: urn.NewPrincipal(urn.PrincipalTypeUser, p.ActorUserID), displayName: nil}, nil
}

const (
	// maxAgentNameAttempts bounds how many names derived from the assistant's
	// name are tried: the name itself, then "<name> (2)" onwards. Assistant
	// names are unique per project but agent names per organization, so a few
	// collisions are expected; past this the agent is named after the
	// assistant's ID, which cannot collide with another assistant's agent.
	maxAgentNameAttempts = 20

	// maxAgentNameLength is the agents.name length limit, in characters.
	maxAgentNameLength = 120
)

// createAgent creates the assistant's agent, owned by owner and named after
// the assistant.
func (s *Service) createAgent(ctx context.Context, tx pgx.Tx, p ProvisionParams, assistantName, owner string, actor auditActor) (agentrepo.Agent, error) {
	q := agentrepo.New(tx)
	params := agentrepo.CreateAgentIfNameAvailableParams{
		OrganizationID: p.OrganizationID,
		OwnerUserID:    owner,
		ProjectID:      uuid.NullUUID{UUID: p.ProjectID, Valid: true},
		Name:           "",
	}
	var agent agentrepo.Agent
	err := pgx.ErrNoRows
	for attempt := 1; attempt <= maxAgentNameAttempts && errors.Is(err, pgx.ErrNoRows); attempt++ {
		params.Name = agentNameCandidate(assistantName, attempt)
		agent, err = q.CreateAgentIfNameAvailable(ctx, params)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		agent, err = q.CreateAgent(ctx, agentrepo.CreateAgentParams{
			OrganizationID: p.OrganizationID,
			OwnerUserID:    owner,
			ProjectID:      params.ProjectID,
			Name:           "Assistant " + p.AssistantID.String(),
		})
	}
	if err != nil {
		return agentrepo.Agent{}, fmt.Errorf("create assistant agent: %w", err)
	}
	if err := s.audit.LogAgent(ctx, tx, audit.LogAgentEvent{
		OrganizationID: p.OrganizationID, AgentURN: urn.NewAgentIdentity(agent.ID.String()), Actor: actor.principal, ActorDisplayName: actor.displayName,
		Action: audit.ActionAgentCreate, Name: agent.Name, Before: nil, After: agentownership.AgentAuditSnapshot(agent),
	}); err != nil {
		return agentrepo.Agent{}, fmt.Errorf("audit assistant agent: %w", err)
	}
	return agent, nil
}

// agentNameCandidate is the assistant name for the first attempt and the name
// with a " (n)" suffix after that, shortened so the result fits an agent name.
func agentNameCandidate(assistantName string, attempt int) string {
	if attempt <= 1 {
		return assistantName
	}
	suffix := fmt.Sprintf(" (%d)", attempt)
	base := []rune(assistantName)
	if limit := maxAgentNameLength - len(suffix); len(base) > limit {
		base = base[:limit]
	}
	return strings.TrimRight(string(base), " ") + suffix
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

func (s *Service) bindExistingRoots(ctx context.Context, tx pgx.Tx, p ProvisionParams, actor auditActor) error {
	// Locks every root in ID order before any issuer lock is taken.
	roots, err := repo.New(tx).ListAssistantRoots(ctx, repo.ListAssistantRootsParams{ProjectID: p.ProjectID, AssistantID: p.AssistantID.String()})
	if err != nil {
		return fmt.Errorf("list assistant root triggers: %w", err)
	}
	for _, root := range roots {
		if err := s.bindRoot(ctx, tx, p.ProjectID, root, false, actor); err != nil {
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
