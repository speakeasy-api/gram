package agentmanagement

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
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
		human, agent, err := s.authorizer.RequireAgentForUpdate(ctx, tx, agentID, OwnedAgentSetup)
		if err != nil {
			return err
		}
		row, err := repo.New(tx).CreateAgentPolicyGrant(ctx, repo.CreateAgentPolicyGrantParams{
			OrganizationID: human.Auth.ActiveOrganizationID,
			AgentID:        agent.ID,
			Scope:          string(scope),
			Selectors:      selectorRaw,
		})
		if err != nil {
			return mapWriteError(err, "agent policy grant already exists")
		}
		grantRow := policyGrantRow{ID: row.ID, Scope: row.Scope, Selectors: row.Selectors, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
		result, err = policyGrantView(grantRow)
		if err != nil {
			return err
		}
		return s.logPolicyGrant(ctx, tx, human, agent, audit.ActionAgentPolicyGrantCreate, nil, policyGrantAuditSnapshot(grantRow))
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, string(audit.ActionAgentPolicyGrantCreate))
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
		human, agent, err := s.authorizer.RequireAgentForUpdate(ctx, tx, agentID, OwnedAgentSetup)
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

		row, err := q.UpdateAgentPolicyGrant(ctx, repo.UpdateAgentPolicyGrantParams{
			OrganizationID: human.Auth.ActiveOrganizationID, AgentID: agent.ID, GrantID: grantID,
			Scope: string(scope), Selectors: selectorRaw,
		})
		if err != nil {
			return mapWriteError(err, "agent policy grant already exists")
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
		row, err := repo.New(tx).DeleteAgentPolicyGrant(ctx, repo.DeleteAgentPolicyGrantParams{
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

func validatePolicyGrant(rawScope, effect string, input *gen.AgentPolicySelector) (authz.Scope, []byte, error) {
	if effect != "allow" {
		return "", nil, oops.E(oops.CodeBadRequest, nil, "agent policy grants must use allow effect")
	}
	scope := authz.Scope(rawScope)
	if err := authz.ValidateAgentRuntimeScope(authz.CurrentAgentRuntimeScopeRegistryVersion, scope); err != nil {
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
		authz.SelectorKeyDisposition:    input.Disposition,
		authz.SelectorKeyTool:           input.Tool,
		authz.SelectorKeyProjectID:      input.ProjectID,
		authz.SelectorKeyServerURL:      input.ServerURL,
		authz.SelectorKeyServerIdentity: input.ServerIdentity,
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
		ResourceKind:   selector[authz.SelectorKeyResourceKind],
		ResourceID:     selector[authz.SelectorKeyResourceID],
		Disposition:    nil,
		Tool:           nil,
		ProjectID:      nil,
		ServerURL:      nil,
		ServerIdentity: nil,
	}
	for key, target := range map[string]**string{
		authz.SelectorKeyDisposition:    &result.Disposition,
		authz.SelectorKeyTool:           &result.Tool,
		authz.SelectorKeyProjectID:      &result.ProjectID,
		authz.SelectorKeyServerURL:      &result.ServerURL,
		authz.SelectorKeyServerIdentity: &result.ServerIdentity,
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
