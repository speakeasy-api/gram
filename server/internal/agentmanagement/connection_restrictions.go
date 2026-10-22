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
)

// connectionRestrictionSelector names one logical connection. The server
// fixes every key, so a connection owner can never write a wildcard, symbolic
// or broader restriction through this authority.
func connectionRestrictionSelector(remoteSessionClientID uuid.UUID, ownerUserID string) authz.Selector {
	return authz.ConnectionAuthClientSelector(remoteSessionClientID.String(), ownerUserID)
}

func (s *Service) ListConnectionRestrictions(ctx context.Context, _ *gen.ListConnectionRestrictionsPayload) (*gen.ListAgentConnectionRestrictionsResult, error) {
	human, err := s.authorizer.RequireHuman(ctx, s.db)
	if err != nil {
		return nil, s.serviceError(ctx, err, "list connection restrictions")
	}
	rows, err := repo.New(s.db).ListConnectionRestrictions(ctx, repo.ListConnectionRestrictionsParams{
		OrganizationID: human.Auth.ActiveOrganizationID,
		OwnerUserID:    human.Auth.UserID,
	})
	if err != nil {
		return nil, s.serviceError(ctx, fmt.Errorf("list connection restrictions: %w", err), "list connection restrictions")
	}
	items := make([]*gen.AgentConnectionRestriction, 0, len(rows))
	for _, row := range rows {
		items = append(items, connectionRestrictionView(row))
	}
	return &gen.ListAgentConnectionRestrictionsResult{Items: items}, nil
}

func (s *Service) RestrictAgentOnConnection(ctx context.Context, payload *gen.RestrictAgentOnConnectionPayload) (*gen.AgentConnectionRestriction, error) {
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return nil, err
	}
	remoteSessionClientID, err := uuid.Parse(payload.RemoteSessionClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid upstream client id")
	}

	var result *gen.AgentConnectionRestriction
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, err := s.authorizer.RequireHuman(ctx, tx)
		if err != nil {
			return err
		}
		organizationID, ownerUserID := human.Auth.ActiveOrganizationID, human.Auth.UserID
		q := repo.New(tx)

		// Only the agent's tenant is checked: blocking an agent from your own
		// connection needs no authority over the agent.
		agent, err := q.GetAgentByID(ctx, repo.GetAgentByIDParams{OrganizationID: organizationID, ID: agentID})
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, nil, "agent not found")
		}
		if err != nil {
			return fmt.Errorf("load agent: %w", err)
		}
		owned, err := q.HasOwnedPersonalConnection(ctx, repo.HasOwnedPersonalConnectionParams{
			UserID: ownerUserID, RemoteSessionClientID: remoteSessionClientID, OrganizationID: organizationID,
		})
		if err != nil {
			return fmt.Errorf("check connection ownership: %w", err)
		}
		if !owned {
			return oops.E(oops.CodeNotFound, nil, "connection not found")
		}

		existing, err := q.GetConnectionRestriction(ctx, repo.GetConnectionRestrictionParams{
			OrganizationID: organizationID, AgentID: agent.ID, OwnerUserID: ownerUserID, RemoteSessionClientID: remoteSessionClientID,
		})
		switch {
		case err == nil:
			result = connectionRestrictionView(existing)
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("load connection restriction: %w", err)
		}

		selector := connectionRestrictionSelector(remoteSessionClientID, ownerUserID)
		selectorRaw, err := selector.MarshalJSON()
		if err != nil {
			return fmt.Errorf("marshal connection restriction selector: %w", err)
		}
		inserted, err := q.CreateConnectionRestrictionGrant(ctx, repo.CreateConnectionRestrictionGrantParams{
			OrganizationID: organizationID, AgentID: agent.ID, Selectors: selectorRaw,
		})
		if err != nil {
			return fmt.Errorf("write connection restriction grant: %w", err)
		}
		// An agent manager may already hold the same grant; the restriction then
		// records the owner's authority beside theirs, and removing it later
		// leaves their grant in place.
		row, err := q.RecordConnectionRestriction(ctx, repo.RecordConnectionRestrictionParams{
			OrganizationID: organizationID, AgentID: agent.ID, OwnerUserID: ownerUserID,
			RemoteSessionClientID: remoteSessionClientID, GrantExisted: !inserted,
		})
		if err != nil {
			return fmt.Errorf("record connection restriction: %w", err)
		}
		result = connectionRestrictionView(row)
		return s.logPolicyGrant(ctx, tx, human, agent, audit.ActionAgentConnectionRestrictionCreate, nil, connectionRestrictionAuditSnapshot(row.ID, selector))
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, string(audit.ActionAgentConnectionRestrictionCreate))
	}
	return result, nil
}

func (s *Service) RemoveConnectionRestriction(ctx context.Context, payload *gen.RemoveConnectionRestrictionPayload) error {
	id, err := uuid.Parse(payload.RestrictionID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid connection restriction id")
	}

	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, err := s.authorizer.RequireHuman(ctx, tx)
		if err != nil {
			return err
		}
		organizationID := human.Auth.ActiveOrganizationID
		q := repo.New(tx)
		row, err := q.DeleteConnectionRestriction(ctx, repo.DeleteConnectionRestrictionParams{
			OrganizationID: organizationID, OwnerUserID: human.Auth.UserID, ID: id,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, nil, "connection restriction not found")
		}
		if err != nil {
			return fmt.Errorf("delete connection restriction: %w", err)
		}
		selector := connectionRestrictionSelector(row.RemoteSessionClientID, row.OwnerUserID)
		if !row.TakenOverAt.Valid {
			selectorRaw, err := selector.MarshalJSON()
			if err != nil {
				return fmt.Errorf("marshal connection restriction selector: %w", err)
			}
			if err := q.DeleteConnectionRestrictionGrant(ctx, repo.DeleteConnectionRestrictionGrantParams{
				OrganizationID: organizationID, AgentID: row.AgentID, Selectors: selectorRaw,
			}); err != nil {
				return fmt.Errorf("delete connection restriction grant: %w", err)
			}
		}
		agent, err := q.GetAgentByID(ctx, repo.GetAgentByIDParams{OrganizationID: organizationID, ID: row.AgentID})
		if err != nil {
			return fmt.Errorf("load agent: %w", err)
		}
		return s.logPolicyGrant(ctx, tx, human, agent, audit.ActionAgentConnectionRestrictionDelete, connectionRestrictionAuditSnapshot(row.ID, selector), nil)
	})
	if err != nil {
		return s.serviceError(ctx, err, string(audit.ActionAgentConnectionRestrictionDelete))
	}
	return nil
}

// requireNotConnectionOwnerRestriction refuses agent policy edits to a grant
// a connection owner wrote: only that owner may remove it.
func requireNotConnectionOwnerRestriction(ctx context.Context, tx pgx.Tx, organizationID string, agentID uuid.UUID, scope string, selectors []byte) error {
	held, err := repo.New(tx).IsConnectionOwnerRestriction(ctx, repo.IsConnectionOwnerRestrictionParams{
		OrganizationID: organizationID, AgentID: agentID, Scope: scope, Selectors: selectors,
	})
	if err != nil {
		return fmt.Errorf("check connection owner restriction: %w", err)
	}
	if held {
		return oops.E(oops.CodeConflict, nil, "this restriction was set by the connection's owner, and only they can remove it")
	}
	return nil
}

func connectionRestrictionView(row repo.AgentConnectionRestriction) *gen.AgentConnectionRestriction {
	return &gen.AgentConnectionRestriction{
		ID:                    row.ID.String(),
		AgentID:               row.AgentID.String(),
		RemoteSessionClientID: row.RemoteSessionClientID.String(),
		CreatedAt:             row.CreatedAt.Time.Format(time.RFC3339Nano),
	}
}

func connectionRestrictionAuditSnapshot(id uuid.UUID, selector authz.Selector) *audit.AgentPolicyGrantSnapshot {
	return &audit.AgentPolicyGrantSnapshot{ID: id, Scope: string(authz.ScopeMCPBlockedAuth), Effect: "allow", Selector: map[string]string(selector)}
}
