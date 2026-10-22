package agentmanagement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	srv "github.com/speakeasy-api/gram/server/gen/http/agents/server"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// connectionBlockSelector names one logical connection: ownerUserID's
// connection through the upstream OAuth client, whichever auth client uses it.
// The server fixes every key, so a connection owner can never write a
// wildcard, symbolic or broader block through this authority.
func connectionBlockSelector(remoteSessionClientID uuid.UUID, ownerUserID string) authz.Selector {
	return authz.Selector{
		authz.SelectorKeyResourceKind:    authz.ResourceKindMCPAuthClient,
		authz.SelectorKeyResourceID:      authz.WildcardResource,
		authz.SelectorKeyClientType:      authz.AuthClientTypeRemoteSession,
		authz.SelectorKeyUpstreamClient:  remoteSessionClientID.String(),
		authz.SelectorKeyConnectionOwner: ownerUserID,
	}
}

// requestDecoder rejects unknown body fields on the connection block
// endpoints. A connection owner's authority covers only the fixed block the
// server builds, so a request naming a scope, principal or selector is refused
// rather than silently narrowed to that block.
func requestDecoder(r *http.Request) goahttp.Decoder {
	switch r.URL.Path {
	case srv.BlockAgentOnConnectionAgentsPath(), srv.RemoveConnectionBlockAgentsPath():
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		return decoder
	default:
		return goahttp.RequestDecoder(r)
	}
}

func (s *Service) ListConnectionBlocks(ctx context.Context, payload *gen.ListConnectionBlocksPayload) (*gen.ListAgentConnectionBlocksResult, error) {
	cursor := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.Cursor != nil {
		parsed, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid connection block cursor")
		}
		cursor = uuid.NullUUID{UUID: parsed, Valid: true}
	}
	limit := payload.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return nil, oops.C(oops.CodeBadRequest)
	}
	human, err := s.authorizer.RequireHuman(ctx, s.db)
	if err != nil {
		return nil, s.serviceError(ctx, err, "list connection blocks")
	}
	rows, err := repo.New(s.db).ListConnectionBlocks(ctx, repo.ListConnectionBlocksParams{
		OrganizationID: human.Auth.ActiveOrganizationID,
		OwnerUserID:    human.Auth.UserID,
		Cursor:         cursor,
		LimitValue:     int32(limit + 1),
	})
	if err != nil {
		return nil, s.serviceError(ctx, fmt.Errorf("list connection blocks: %w", err), "list connection blocks")
	}
	result := &gen.ListAgentConnectionBlocksResult{Items: make([]*gen.AgentConnectionBlock, 0, len(rows)), NextCursor: nil}
	if len(rows) > limit {
		rows = rows[:limit]
		value := rows[len(rows)-1].ID.String()
		result.NextCursor = &value
	}
	for _, row := range rows {
		result.Items = append(result.Items, connectionBlockView(row))
	}
	return result, nil
}

func (s *Service) BlockAgentOnConnection(ctx context.Context, payload *gen.BlockAgentOnConnectionPayload) (*gen.AgentConnectionBlock, error) {
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return nil, err
	}
	remoteSessionClientID, err := uuid.Parse(payload.RemoteSessionClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid upstream client id")
	}

	var result *gen.AgentConnectionBlock
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, err := s.authorizer.RequireHuman(ctx, tx)
		if err != nil {
			return err
		}
		organizationID, ownerUserID := human.Auth.ActiveOrganizationID, human.Auth.UserID
		q := repo.New(tx)

		// Only the agent's tenant is checked: blocking an agent from your own
		// connection needs no authority over the agent. The agent lock
		// serializes this with agent policy edits, which take it too, so a
		// manager cannot delete the grant between the checks below and the
		// block being recorded.
		agent, err := q.GetAgentByIDForUpdate(ctx, repo.GetAgentByIDForUpdateParams{OrganizationID: organizationID, ID: agentID})
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

		selector := connectionBlockSelector(remoteSessionClientID, ownerUserID)
		selectorRaw, err := selector.MarshalJSON()
		if err != nil {
			return fmt.Errorf("marshal connection block selector: %w", err)
		}
		// Written on every call, so repeating the request restores the grant
		// of a block already recorded.
		inserted, err := q.CreateConnectionBlockGrant(ctx, repo.CreateConnectionBlockGrantParams{
			OrganizationID: organizationID, AgentID: agent.ID, Selectors: selectorRaw,
		})
		if err != nil {
			return fmt.Errorf("write connection block grant: %w", err)
		}

		existing, err := q.GetConnectionBlock(ctx, repo.GetConnectionBlockParams{
			OrganizationID: organizationID, AgentID: agent.ID, OwnerUserID: ownerUserID, RemoteSessionClientID: remoteSessionClientID,
		})
		switch {
		case err == nil:
			result = connectionBlockView(existing)
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("load connection block: %w", err)
		}

		// An agent manager may already hold the same grant; the block then
		// records the owner's authority beside theirs, and removing it later
		// leaves their grant in place.
		row, err := q.RecordConnectionBlock(ctx, repo.RecordConnectionBlockParams{
			OrganizationID: organizationID, AgentID: agent.ID, OwnerUserID: ownerUserID,
			RemoteSessionClientID: remoteSessionClientID, GrantExisted: !inserted,
		})
		if err != nil {
			return fmt.Errorf("record connection block: %w", err)
		}
		result = connectionBlockView(row)
		return s.logPolicyGrant(ctx, tx, human, agent, audit.ActionAgentConnectionBlockCreate, nil, connectionBlockAuditSnapshot(row.ID, selector))
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, string(audit.ActionAgentConnectionBlockCreate))
	}
	return result, nil
}

func (s *Service) RemoveConnectionBlock(ctx context.Context, payload *gen.RemoveConnectionBlockPayload) error {
	id, err := uuid.Parse(payload.BlockID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid connection block id")
	}

	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, err := s.authorizer.RequireHuman(ctx, tx)
		if err != nil {
			return err
		}
		organizationID, ownerUserID := human.Auth.ActiveOrganizationID, human.Auth.UserID
		q := repo.New(tx)
		block, err := q.GetOwnedConnectionBlock(ctx, repo.GetOwnedConnectionBlockParams{
			OrganizationID: organizationID, OwnerUserID: ownerUserID, ID: id,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, nil, "connection block not found")
		}
		if err != nil {
			return fmt.Errorf("load connection block: %w", err)
		}
		// Lock as agent policy edits do. A deleted agent is locked too, so its
		// owner can still clean up.
		agent, err := q.LockAgentIncludingDeleted(ctx, repo.LockAgentIncludingDeletedParams{OrganizationID: organizationID, ID: block.AgentID})
		if err != nil {
			return fmt.Errorf("lock agent: %w", err)
		}
		row, err := q.DeleteConnectionBlock(ctx, repo.DeleteConnectionBlockParams{
			OrganizationID: organizationID, OwnerUserID: ownerUserID, ID: id,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, nil, "connection block not found")
		}
		if err != nil {
			return fmt.Errorf("delete connection block: %w", err)
		}
		selector := connectionBlockSelector(row.RemoteSessionClientID, row.OwnerUserID)
		if !row.TakenOverAt.Valid {
			selectorRaw, err := selector.MarshalJSON()
			if err != nil {
				return fmt.Errorf("marshal connection block selector: %w", err)
			}
			if err := q.DeleteConnectionBlockGrant(ctx, repo.DeleteConnectionBlockGrantParams{
				OrganizationID: organizationID, AgentID: row.AgentID, Selectors: selectorRaw,
			}); err != nil {
				return fmt.Errorf("delete connection block grant: %w", err)
			}
		}
		return s.logPolicyGrant(ctx, tx, human, agent, audit.ActionAgentConnectionBlockDelete, connectionBlockAuditSnapshot(row.ID, selector), nil)
	})
	if err != nil {
		return s.serviceError(ctx, err, string(audit.ActionAgentConnectionBlockDelete))
	}
	return nil
}

// requireNotConnectionOwnerBlock refuses agent policy edits to a grant
// a connection owner wrote: only that owner may remove it.
func requireNotConnectionOwnerBlock(ctx context.Context, tx pgx.Tx, organizationID string, agentID uuid.UUID, scope string, selectors []byte) error {
	held, err := repo.New(tx).IsConnectionOwnerBlock(ctx, repo.IsConnectionOwnerBlockParams{
		OrganizationID: organizationID, AgentID: agentID, Scope: scope, Selectors: selectors,
	})
	if err != nil {
		return fmt.Errorf("check connection owner block: %w", err)
	}
	if held {
		return oops.E(oops.CodeConflict, nil, "this block was set by the connection's owner, and only they can remove it")
	}
	return nil
}

func connectionBlockView(row repo.AgentConnectionBlock) *gen.AgentConnectionBlock {
	return &gen.AgentConnectionBlock{
		ID:                    row.ID.String(),
		AgentID:               row.AgentID.String(),
		RemoteSessionClientID: row.RemoteSessionClientID.String(),
		CreatedAt:             row.CreatedAt.Time.Format(time.RFC3339Nano),
	}
}

func connectionBlockAuditSnapshot(id uuid.UUID, selector authz.Selector) *audit.AgentPolicyGrantSnapshot {
	return &audit.AgentPolicyGrantSnapshot{ID: id, Scope: string(authz.ScopeMCPBlockedAuth), Effect: "allow", Selector: map[string]string(selector)}
}
