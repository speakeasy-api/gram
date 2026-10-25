package agentmanagement

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// readableAgentBatchSize is the ListManagedAgents batch read per scan step,
// the largest agents.list page.
const readableAgentBatchSize = maxAgentPageSize

// ReadableAgentIDsNamed returns the agents the caller may read whose name
// contains search. Credentials that cannot manage agents read none.
func (a *Authorizer) ReadableAgentIDsNamed(ctx context.Context, dbtx repo.DBTX, search string) ([]uuid.UUID, error) {
	human, err := a.RequireHuman(ctx, dbtx)
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && (shareable.Code == oops.CodeUnauthorized || shareable.Code == oops.CodeForbidden) {
		return []uuid.UUID{}, nil
	}
	if err != nil {
		return nil, err
	}

	queries := repo.New(dbtx)
	cursorName := pgtype.Text{String: "", Valid: false}
	cursorID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	ids := []uuid.UUID{}
	scanned := 0
	for {
		rows, err := queries.ListManagedAgents(ctx, repo.ListManagedAgentsParams{
			OrganizationID:   human.Auth.ActiveOrganizationID,
			Search:           pgtype.Text{String: conv.EscapeLike(search), Valid: true},
			Lifecycles:       []string{},
			OwnerUserIds:     []string{},
			RegisteredAfter:  pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			RegisteredBefore: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			SortDescending:   false,
			CursorName:       cursorName,
			CursorID:         cursorID,
			PageLimit:        conv.SafeInt32(readableAgentBatchSize),
		})
		if err != nil {
			return nil, fmt.Errorf("list managed agents by name: %w", err)
		}
		scanned += len(rows)

		for _, agent := range rows {
			permissions, err := a.Permissions(ctx, human, agent)
			if err != nil {
				return nil, err
			}
			if permissions.Read {
				ids = append(ids, agent.ID)
			}
		}

		if len(rows) < readableAgentBatchSize {
			return ids, nil
		}
		if scanned >= maxAgentRowsScanned {
			return nil, oops.E(oops.CodeBadRequest, nil, "too many agents match this search; use a longer search")
		}
		last := rows[len(rows)-1]
		cursorName = pgtype.Text{String: strings.ToLower(last.Name), Valid: true}
		cursorID = uuid.NullUUID{UUID: last.ID, Valid: true}
	}
}

// ReadableAgentNames names the given agents the caller may read. Agents the
// caller may not read, deleted agents, and agents of other organizations are
// left out, as are all of them for credentials that cannot manage agents.
func (a *Authorizer) ReadableAgentNames(ctx context.Context, dbtx repo.DBTX, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	names := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return names, nil
	}
	human, err := a.RequireHuman(ctx, dbtx)
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && (shareable.Code == oops.CodeUnauthorized || shareable.Code == oops.CodeForbidden) {
		return names, nil
	}
	if err != nil {
		return nil, err
	}
	agents, err := repo.New(dbtx).ListAgentsByIDs(ctx, repo.ListAgentsByIDsParams{OrganizationID: human.Auth.ActiveOrganizationID, Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("list agents by id: %w", err)
	}
	for _, agent := range agents {
		permissions, err := a.Permissions(ctx, human, agent)
		if err != nil {
			return nil, err
		}
		if permissions.Read {
			names[agent.ID] = agent.Name
		}
	}
	return names, nil
}
