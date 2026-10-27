package agentmanagement

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type policyGrantRow struct {
	ID        uuid.UUID
	Scope     string
	Selectors []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s *Service) ListPolicyGrants(ctx context.Context, payload *gen.ListPolicyGrantsPayload) ([]*gen.AgentPolicyGrant, error) {
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return nil, err
	}

	human, agent, err := s.authorizer.RequireAgent(ctx, s.db, agentID, OwnedAgentSetup)
	if err != nil {
		return nil, s.serviceError(ctx, err, "list agent policy grants")
	}
	rows, err := repo.New(s.db).ListAgentPolicyGrants(ctx, repo.ListAgentPolicyGrantsParams{
		OrganizationID: human.Auth.ActiveOrganizationID,
		AgentID:        agent.ID,
	})
	if err != nil {
		return nil, s.serviceError(ctx, fmt.Errorf("list agent policy grants: %w", err), "list agent policy grants")
	}

	result := make([]*gen.AgentPolicyGrant, 0, len(rows))
	for _, row := range rows {
		grant, err := policyGrantView(policyGrantRow{
			ID: row.ID, Scope: row.Scope, Selectors: row.Selectors,
			CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
		})
		if err != nil {
			return nil, s.serviceError(ctx, err, "list agent policy grants")
		}
		result = append(result, grant)
	}
	return result, nil
}

func (s *Service) CreatePolicyGrant(ctx context.Context, payload *gen.CreatePolicyGrantPayload) (*gen.AgentPolicyGrant, error) {
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return nil, err
	}
	scope, selectorRaw, err := validatePolicyGrant(payload.Scope, payload.Effect, payload.Selector)
	if err != nil {
		return nil, err
	}

	var result *gen.AgentPolicyGrant
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, agent, err := s.authorizer.RequireAgentOwnerForUpdate(ctx, tx, agentID, OwnedAgentSetup)
		if err != nil {
			return err
		}
		result, err = s.createPolicyGrant(ctx, tx, human, agent, scope, selectorRaw)
		return err
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, string(audit.ActionAgentPolicyGrantCreate))
	}
	return result, nil
}

func (s *Service) createPolicyGrant(ctx context.Context, tx pgx.Tx, human HumanContext, agent repo.Agent, scope authz.Scope, selectorRaw []byte) (*gen.AgentPolicyGrant, error) {
	if err := requirePolicyAuthority(ctx, tx, human, agent, scope, selectorRaw); err != nil {
		return nil, err
	}

	row, err := repo.New(tx).CreateAgentPolicyGrant(ctx, repo.CreateAgentPolicyGrantParams{
		OrganizationID: human.Auth.ActiveOrganizationID,
		AgentID:        agent.ID,
		Scope:          string(scope),
		Selectors:      selectorRaw,
	})
	if err != nil {
		return nil, mapWriteError(err, "agent policy grant already exists")
	}
	if err := authz.MarkAuthPermissionTakenOverTx(ctx, tx, human.Auth.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String(), row.Scope, row.Selectors); err != nil {
		return nil, err //nolint:wrapcheck // already wrapped
	}
	grantRow := policyGrantRow{ID: row.ID, Scope: row.Scope, Selectors: row.Selectors, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	result, err := policyGrantView(grantRow)
	if err != nil {
		return nil, err
	}
	if err := s.logPolicyGrant(ctx, tx, human, agent, audit.ActionAgentPolicyGrantCreate, nil, policyGrantAuditSnapshot(grantRow)); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) UpdatePolicyGrant(ctx context.Context, payload *gen.UpdatePolicyGrantPayload) (*gen.AgentPolicyGrant, error) {
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return nil, err
	}
	grantID, err := parseGrantID(payload.GrantID)
	if err != nil {
		return nil, err
	}
	scope, selectorRaw, err := validatePolicyGrant(payload.Scope, payload.Effect, payload.Selector)
	if err != nil {
		return nil, err
	}

	var result *gen.AgentPolicyGrant
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, agent, err := s.authorizer.RequireAgentOwnerForUpdate(ctx, tx, agentID, OwnedAgentSetup)
		if err != nil {
			return err
		}
		q := repo.New(tx)
		beforeRow, err := q.GetAgentPolicyGrantForUpdate(ctx, repo.GetAgentPolicyGrantForUpdateParams{
			OrganizationID: human.Auth.ActiveOrganizationID, AgentID: agent.ID, GrantID: grantID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, nil, "agent policy grant not found")
		}
		if err != nil {
			return fmt.Errorf("load agent policy grant: %w", err)
		}
		before := policyGrantRow{ID: beforeRow.ID, Scope: beforeRow.Scope, Selectors: beforeRow.Selectors, CreatedAt: beforeRow.CreatedAt.Time, UpdatedAt: beforeRow.UpdatedAt.Time}
		if err := requireNotConnectionOwnerBlock(ctx, tx, human.Auth.ActiveOrganizationID, agent.ID, beforeRow.Scope, beforeRow.Selectors); err != nil {
			return err
		}

		if err := requirePolicyAuthority(ctx, tx, human, agent, scope, selectorRaw); err != nil {
			return err
		}

		row, err := q.UpdateAgentPolicyGrant(ctx, repo.UpdateAgentPolicyGrantParams{
			OrganizationID: human.Auth.ActiveOrganizationID, AgentID: agent.ID, GrantID: grantID,
			Scope: string(scope), Selectors: selectorRaw,
		})
		if err != nil {
			return mapWriteError(err, "agent policy grant already exists")
		}
		if err := authz.MarkAuthPermissionTakenOverTx(ctx, tx, human.Auth.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String(), row.Scope, row.Selectors); err != nil {
			return err //nolint:wrapcheck // already wrapped
		}
		after := policyGrantRow{ID: row.ID, Scope: row.Scope, Selectors: row.Selectors, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
		result, err = policyGrantView(after)
		if err != nil {
			return err
		}
		return s.logPolicyGrant(ctx, tx, human, agent, audit.ActionAgentPolicyGrantUpdate, policyGrantAuditSnapshot(before), policyGrantAuditSnapshot(after))
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, string(audit.ActionAgentPolicyGrantUpdate))
	}
	return result, nil
}

func (s *Service) DeletePolicyGrant(ctx context.Context, payload *gen.DeletePolicyGrantPayload) error {
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return err
	}
	grantID, err := parseGrantID(payload.GrantID)
	if err != nil {
		return err
	}

	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, agent, err := s.authorizer.RequireAgentForUpdate(ctx, tx, agentID, OwnedAgentSetup)
		if err != nil {
			return err
		}
		q := repo.New(tx)
		existing, err := q.GetAgentPolicyGrantForUpdate(ctx, repo.GetAgentPolicyGrantForUpdateParams{
			OrganizationID: human.Auth.ActiveOrganizationID, AgentID: agent.ID, GrantID: grantID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, nil, "agent policy grant not found")
		}
		if err != nil {
			return fmt.Errorf("load agent policy grant: %w", err)
		}
		if err := requireNotConnectionOwnerBlock(ctx, tx, human.Auth.ActiveOrganizationID, agent.ID, existing.Scope, existing.Selectors); err != nil {
			return err
		}
		if expected := payload.ExpectedPolicy; expected != nil {
			if expected.Selector == nil || expected.Scope == "" {
				return oops.E(oops.CodeBadRequest, nil, "expected policy scope and selector are required")
			}
			current, err := authz.SelectorFromRow(existing.Selectors)
			if err != nil {
				return fmt.Errorf("parse current agent policy selector: %w", err)
			}
			// These required API fields serialize absent legacy keys as empty strings.
			// Preserve every other key and all nonempty values for exact comparison.
			for _, key := range []string{authz.SelectorKeyResourceKind, authz.SelectorKeyResourceID} {
				if _, ok := current[key]; !ok {
					if current == nil {
						current = make(authz.Selector)
					}
					current[key] = ""
				}
			}
			if existing.Scope != expected.Scope || !maps.Equal(current, policySelector(expected.Selector)) {
				return oops.E(oops.CodeConflict, nil, "agent policy grant changed; reload before deleting")
			}
		}
		row, err := q.DeleteAgentPolicyGrant(ctx, repo.DeleteAgentPolicyGrantParams{
			OrganizationID: human.Auth.ActiveOrganizationID, AgentID: agent.ID, GrantID: grantID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, nil, "agent policy grant not found")
		}
		if err != nil {
			return fmt.Errorf("delete agent policy grant: %w", err)
		}
		before := policyGrantRow{ID: row.ID, Scope: row.Scope, Selectors: row.Selectors, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
		return s.logPolicyGrant(ctx, tx, human, agent, audit.ActionAgentPolicyGrantDelete, policyGrantAuditSnapshot(before), nil)
	})
	if err != nil {
		return s.serviceError(ctx, err, string(audit.ActionAgentPolicyGrantDelete))
	}
	return nil
}

// CheckPolicyGrants answers, for each grant, whether a policy write would
// accept it, and writes nothing. It authorizes and decides exactly as the
// writes do, so a client can withhold a choice the write would refuse instead
// of offering it and showing the refusal afterwards.
func (s *Service) CheckPolicyGrants(ctx context.Context, payload *gen.CheckPolicyGrantsPayload) (*gen.CheckAgentPolicyGrantsResult, error) {
	type requestedGrant struct {
		scope       authz.Scope
		selectorRaw []byte
	}
	requested := make([]requestedGrant, 0, len(payload.PolicyGrants))
	for _, grant := range payload.PolicyGrants {
		if grant == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "agent policy grant is required")
		}
		scope, selectorRaw, err := validatePolicyGrant(grant.Scope, grant.Effect, grant.Selector)
		if err != nil {
			return nil, err
		}
		requested = append(requested, requestedGrant{scope: scope, selectorRaw: selectorRaw})
	}
	var agentID *uuid.UUID
	if payload.AgentID != nil {
		parsed, err := parseAgentID(*payload.AgentID)
		if err != nil {
			return nil, err
		}
		agentID = &parsed
	}
	// As agents.create reads it: empty means the caller.
	ownerUserID := ""
	if payload.OwnerUserID != nil {
		ownerUserID = *payload.OwnerUserID
	}
	if agentID != nil && ownerUserID != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "owner_user_id cannot be combined with agent_id")
	}

	results := make([]*gen.AgentPolicyGrantCheck, 0, len(requested))
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, ownerUserID, err := s.policyCheckOwner(ctx, tx, agentID, ownerUserID)
		if err != nil {
			return err
		}
		checker := policyAuthorityChecker{human: human, ownerUserID: ownerUserID, ownerGrants: nil, ownerGrantsLoaded: false}
		for _, grant := range requested {
			limit, err := checker.limit(ctx, tx, grant.scope, grant.selectorRaw)
			if err != nil {
				return err
			}
			check := &gen.AgentPolicyGrantCheck{Allowed: limit == policyLimitNone, LimitedBy: nil}
			if limit != policyLimitNone {
				check.LimitedBy = new(string(limit))
			}
			results = append(results, check)
		}
		return nil
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, "check agent policy grants")
	}
	return &gen.CheckAgentPolicyGrantsResult{Results: results}, nil
}

// policyCheckOwner authorizes a policy check and returns the owner whose
// permissions bound it. With an agent it authorizes as a policy write does,
// pinning both memberships before the agent lock. Without one it authorizes as
// agents.create does for ownerUserID, the caller when empty: the grants are
// for an agent the caller is about to create.
func (s *Service) policyCheckOwner(ctx context.Context, tx pgx.Tx, agentID *uuid.UUID, ownerUserID string) (HumanContext, string, error) {
	if agentID == nil {
		authCtx, ok := contextAuth(ctx)
		if !ok {
			return HumanContext{}, "", oops.C(oops.CodeUnauthorized)
		}
		if ownerUserID == "" {
			ownerUserID = authCtx.UserID
		}
		// Never stored: it stands for the agent agents.create would make, so
		// naming another owner needs the same agent:write there as here.
		prospective, err := uuid.NewV7()
		if err != nil {
			return HumanContext{}, "", fmt.Errorf("generate prospective agent id: %w", err)
		}
		human, err := s.authorizer.RequireCreate(ctx, tx, prospective, ownerUserID)
		if err != nil {
			return HumanContext{}, "", err
		}
		return human, ownerUserID, nil
	}
	human, agent, err := s.authorizer.RequireAgentOwnerForUpdate(ctx, tx, *agentID, OwnedAgentSetup)
	if err != nil {
		return HumanContext{}, "", err
	}
	return human, agent.OwnerUserID, nil
}

// policyLimit names whose permissions a requested grant exceeds.
type policyLimit string

const (
	// policyLimitNone means the grant fits within the caller's and the owner's
	// permissions.
	policyLimitNone policyLimit = ""

	// policyLimitCaller means the grant exceeds the configuring person's
	// permissions.
	policyLimitCaller policyLimit = "caller"

	// policyLimitOwner means the grant exceeds the agent owner's permissions.
	policyLimitOwner policyLimit = "owner"
)

// requirePolicyAuthority checks that the caller may write one grant.
func requirePolicyAuthority(ctx context.Context, tx pgx.Tx, human HumanContext, agent repo.Agent, scope authz.Scope, selectorRaw []byte) error {
	checker := policyAuthorityChecker{human: human, ownerUserID: agent.OwnerUserID, ownerGrants: nil, ownerGrantsLoaded: false}
	limit, err := checker.limit(ctx, tx, scope, selectorRaw)
	if err != nil {
		return err
	}
	if limit == policyLimitCaller {
		return oops.E(oops.CodeForbidden, nil, "requested agent access exceeds your permissions; choose a narrower scope or ask an administrator for access")
	}
	if limit == policyLimitOwner {
		return oops.E(oops.CodeForbidden, nil, "requested agent access exceeds the owner's permissions; choose a narrower scope or ask an administrator to review the owner's access")
	}
	return nil
}

// policyAuthorityChecker shares the owner's loaded permissions across one
// batch. It must be created anew for each transaction after the caller and
// owner memberships are pinned, so later checks and writes reload permissions.
type policyAuthorityChecker struct {
	human             HumanContext
	ownerUserID       string
	ownerGrants       []authz.Grant
	ownerGrantsLoaded bool
}

// limit reports whose permissions stop a policy write. An mcp:blocked_auth
// restriction only withdraws access, so writing one needs no containment;
// every other grant must be contained.
func (c *policyAuthorityChecker) limit(ctx context.Context, tx pgx.Tx, scope authz.Scope, selectorRaw []byte) (policyLimit, error) {
	if err := authz.RequireUpstreamSelectorWritesRaw(ctx, c.human.Auth.ActiveOrganizationID, selectorRaw); err != nil {
		return policyLimitNone, fmt.Errorf("check upstream_client write gate: %w", err)
	}
	if scope == authz.ScopeMCPBlockedAuth {
		return policyLimitNone, nil
	}
	return c.containmentLimit(ctx, tx, scope, selectorRaw)
}

// containmentLimit checks the complete authority of one allow grant,
// including implied scopes and overlapping exclusions. The caller and current
// owner memberships must be pinned before the agent lock is acquired. No agent
// policy is a parent here: setup may expand policy, but never human authority.
func (c *policyAuthorityChecker) containmentLimit(ctx context.Context, tx pgx.Tx, scope authz.Scope, selectorRaw []byte) (policyLimit, error) {
	selector, err := authz.SelectorFromRow(selectorRaw)
	if err != nil {
		return policyLimitNone, fmt.Errorf("parse requested policy selector: %w", err)
	}
	policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, []authz.Grant{authz.NewGrantWithSelector(scope, selector)})
	if err != nil {
		return policyLimitNone, fmt.Errorf("construct requested policy authority: %w", err)
	}
	contained, err := runtimepolicy.DelegationContained(policy, c.human.grants)
	if err != nil {
		return policyLimitNone, fmt.Errorf("check caller policy containment: %w", err)
	}
	if !contained {
		return policyLimitCaller, nil
	}
	if c.ownerUserID == c.human.Auth.UserID {
		return policyLimitNone, nil
	}
	if !c.ownerGrantsLoaded {
		principals, err := authz.ResolveUserPrincipals(ctx, tx, c.human.Auth.ActiveOrganizationID, c.ownerUserID)
		if err != nil {
			return policyLimitNone, fmt.Errorf("resolve policy owner principals: %w", err)
		}
		c.ownerGrants, err = authz.LoadGrants(ctx, tx, c.human.Auth.ActiveOrganizationID, principals)
		if err != nil {
			return policyLimitNone, fmt.Errorf("load policy owner grants: %w", err)
		}
		c.ownerGrantsLoaded = true
	}
	contained, err = runtimepolicy.DelegationContained(policy, c.ownerGrants)
	if err != nil {
		return policyLimitNone, fmt.Errorf("check owner policy containment: %w", err)
	}
	if !contained {
		return policyLimitOwner, nil
	}
	return policyLimitNone, nil
}

func validatePolicyGrant(rawScope, effect string, input *gen.AgentPolicySelector) (authz.Scope, []byte, error) {
	if effect != "allow" {
		return "", nil, oops.E(oops.CodeBadRequest, nil, "agent policy grants must use allow effect")
	}
	scope := authz.Scope(rawScope)
	// Auth client restrictions are part of agent policy; other agent
	// restrictions are written through the resource audience surface.
	if authz.IsBlocklistScope(scope) && scope != authz.ScopeMCPBlockedAuth {
		return "", nil, oops.E(oops.CodeBadRequest, nil, "agent policy grants cannot use blocklist scopes")
	}
	if err := runtimepolicy.ValidateRuntimeScope(runtimepolicy.CurrentRuntimeScopeRegistryVersion, scope); err != nil {
		return "", nil, oops.E(oops.CodeBadRequest, err, "scope is not allowed for agent policy")
	}
	if input == nil {
		return "", nil, oops.E(oops.CodeBadRequest, nil, "agent policy selector is required")
	}
	selector := policySelector(input)
	if err := authz.ValidateSelector(scope, selector); err != nil {
		return "", nil, oops.E(oops.CodeBadRequest, err, "invalid agent policy selector")
	}
	raw, err := selector.MarshalJSON()
	if err != nil {
		return "", nil, fmt.Errorf("marshal agent policy selector: %w", err)
	}
	return scope, raw, nil
}

func policySelector(input *gen.AgentPolicySelector) authz.Selector {
	selector := authz.Selector{
		authz.SelectorKeyResourceKind: input.ResourceKind,
		authz.SelectorKeyResourceID:   input.ResourceID,
	}
	for key, value := range map[string]*string{
		authz.SelectorKeyDisposition:     input.Disposition,
		authz.SelectorKeyTool:            input.Tool,
		authz.SelectorKeyProjectID:       input.ProjectID,
		authz.SelectorKeyServerURL:       input.ServerURL,
		authz.SelectorKeyServerIdentity:  input.ServerIdentity,
		authz.SelectorKeyClientType:      input.ClientType,
		authz.SelectorKeyConnectionOwner: input.ConnectionOwner,
		authz.SelectorKeyUpstreamClient:  input.UpstreamClient,
	} {
		if value != nil {
			selector[key] = *value
		}
	}
	return selector
}

func policyGrantView(row policyGrantRow) (*gen.AgentPolicyGrant, error) {
	selector, err := authz.SelectorFromRow(row.Selectors)
	if err != nil {
		return nil, fmt.Errorf("parse agent policy selector: %w", err)
	}
	return &gen.AgentPolicyGrant{
		ID: row.ID.String(), Scope: row.Scope, Effect: "allow", Selector: policySelectorView(selector),
		CreatedAt: row.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: row.UpdatedAt.Format(time.RFC3339Nano),
	}, nil
}

func policySelectorView(selector authz.Selector) *gen.AgentPolicySelector {
	result := &gen.AgentPolicySelector{
		ResourceKind:    selector[authz.SelectorKeyResourceKind],
		ResourceID:      selector[authz.SelectorKeyResourceID],
		Disposition:     nil,
		Tool:            nil,
		ProjectID:       nil,
		ServerURL:       nil,
		ServerIdentity:  nil,
		ClientType:      nil,
		ConnectionOwner: nil,
		UpstreamClient:  nil,
	}
	for key, target := range map[string]**string{
		authz.SelectorKeyDisposition:     &result.Disposition,
		authz.SelectorKeyTool:            &result.Tool,
		authz.SelectorKeyProjectID:       &result.ProjectID,
		authz.SelectorKeyServerURL:       &result.ServerURL,
		authz.SelectorKeyServerIdentity:  &result.ServerIdentity,
		authz.SelectorKeyClientType:      &result.ClientType,
		authz.SelectorKeyConnectionOwner: &result.ConnectionOwner,
		authz.SelectorKeyUpstreamClient:  &result.UpstreamClient,
	} {
		if value, ok := selector[key]; ok {
			value := value
			*target = &value
		}
	}
	return result
}

func policyGrantAuditSnapshot(row policyGrantRow) *audit.AgentPolicyGrantSnapshot {
	selector, err := authz.SelectorFromRow(row.Selectors)
	if err != nil {
		return &audit.AgentPolicyGrantSnapshot{ID: row.ID, Scope: row.Scope, Effect: "allow", Selector: nil}
	}
	return &audit.AgentPolicyGrantSnapshot{ID: row.ID, Scope: row.Scope, Effect: "allow", Selector: map[string]string(selector)}
}

func (s *Service) logPolicyGrant(ctx context.Context, tx pgx.Tx, human HumanContext, agent repo.Agent, action audit.Action, before, after *audit.AgentPolicyGrantSnapshot) error {
	if err := s.audit.LogAgentPolicyGrant(ctx, tx, audit.LogAgentPolicyGrantEvent{
		OrganizationID:   human.Auth.ActiveOrganizationID,
		AgentURN:         urn.NewAgentIdentity(agent.ID.String()),
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, human.Auth.UserID),
		ActorDisplayName: human.Auth.Email,
		Action:           action, Name: agent.Name, Before: before, After: after,
	}); err != nil {
		return fmt.Errorf("log %s audit event: %w", action, err)
	}
	return nil
}

func parseGrantID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeBadRequest, err, "invalid policy grant id")
	}
	return id, nil
}
