package agentmanagement

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	defaultAgentPageSize = 50
	maxAgentPageSize     = 200
	// Readability is decided in Go, after the query, so a batch can come back
	// entirely invisible while rows the caller may read remain further down.
	// The loop below keeps reading until it has filled a page, and this bounds
	// the work one request may do.
	//
	// Rows, not batches: a batch bound would make the limit the caller chose
	// decide how far the scan reaches. The ceiling is far above any real
	// inventory, and hitting it is reported rather than papered over — a short
	// page would read as the end of the list, and a cursor naming the last row
	// scanned would name an agent the caller is not allowed to see.
	maxAgentRowsScanned = 20_000
)

// List uses the same independent grant-or-ownership decisions as selected-agent
// reads. Runtime eligibility is not a management filter: suspended and revoked
// agents must remain visible so humans can inspect and manage them.
func (s *Service) List(ctx context.Context, payload *gen.ListPayload) (*gen.ListAgentsResult, error) {
	human, err := s.authorizer.RequireHuman(ctx, s.db)
	if err != nil {
		return nil, s.serviceError(ctx, err, "list managed agents")
	}

	// Goa applies the design default, so a zero only reaches here from a caller
	// that constructed the payload itself.
	limit := defaultAgentPageSize
	if payload != nil && payload.Limit != 0 {
		limit = payload.Limit
	}
	if limit < 1 || limit > maxAgentPageSize {
		return nil, oops.E(oops.CodeBadRequest, nil, "limit must be between 1 and %d", maxAgentPageSize)
	}

	// Name order is the only order the agent roster offers, so direction is the
	// whole of its sort. The cursor is keyed to it: a page taken in one
	// direction cannot be resumed in the other.
	descending := payload != nil && payload.NameOrder == "desc"

	search := pgtype.Text{String: "", Valid: false}
	if payload != nil && payload.Search != nil {
		if trimmed := strings.TrimSpace(*payload.Search); trimmed != "" {
			// A bare LIKE pattern from the caller would let them match on
			// wildcards they did not type.
			escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(trimmed)
			search = pgtype.Text{String: escaped, Valid: true}
		}
	}

	// Empty arrays mean "no filter", which is what the query's cardinality
	// check reads them as.
	// A nil slice reaches Postgres as NULL, and cardinality(NULL) is NULL
	// rather than 0, which turns the "no filter" check into NULL and drops
	// every row. These must be empty slices, never nil.
	lifecycles := []string{}
	owners := []string{}
	if payload != nil {
		if payload.Lifecycle != nil {
			lifecycles = payload.Lifecycle
		}
		if payload.OwnerUserIds != nil {
			owners = payload.OwnerUserIds
		}
	}

	registeredAfter, err := optionalMoment(payload, func(p *gen.ListPayload) *string {
		return p.RegisteredAfter
	})
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid registered_after")
	}
	registeredBefore, err := optionalMoment(payload, func(p *gen.ListPayload) *string {
		return p.RegisteredBefore
	})
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid registered_before")
	}

	cursorName := pgtype.Text{String: "", Valid: false}
	cursorID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload != nil && payload.Cursor != nil && *payload.Cursor != "" {
		name, id, err := decodeAgentCursor(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid agent cursor")
		}
		cursorName = pgtype.Text{String: name, Valid: true}
		cursorID = uuid.NullUUID{UUID: id, Valid: true}
	}

	queries := repo.New(s.db)
	// One row beyond the page is what says whether a next cursor exists.
	readable := make([]repo.Agent, 0, limit+1)
	permissionsByID := make(map[string]AgentPermissions, limit+1)
	ownerIDs := make([]string, 0, limit+1)
	seenOwners := make(map[string]bool)

	scanned := 0
	for len(readable) <= limit {
		rows, err := queries.ListManagedAgents(ctx, repo.ListManagedAgentsParams{
			OrganizationID:   human.Auth.ActiveOrganizationID,
			Search:           search,
			Lifecycles:       lifecycles,
			OwnerUserIds:     owners,
			RegisteredAfter:  registeredAfter,
			RegisteredBefore: registeredBefore,
			SortDescending:   descending,
			CursorName:       cursorName,
			CursorID:         cursorID,
			PageLimit:        conv.SafeInt32(limit + 1),
		})
		if err != nil {
			return nil, s.serviceError(ctx, fmt.Errorf("list managed agents: %w", err), "list managed agents")
		}
		if len(rows) == 0 {
			break
		}
		scanned += len(rows)

		for _, agent := range rows {
			permissions, err := s.authorizer.Permissions(ctx, human, agent)
			if err != nil {
				return nil, s.serviceError(ctx, err, "evaluate agent permissions")
			}
			if !permissions.Read {
				continue
			}
			readable = append(readable, agent)
			permissionsByID[agent.ID.String()] = permissions
			if !seenOwners[agent.OwnerUserID] {
				seenOwners[agent.OwnerUserID] = true
				ownerIDs = append(ownerIDs, agent.OwnerUserID)
			}
		}

		// Short read means the table is exhausted, whatever was visible in it.
		if len(rows) < limit+1 {
			break
		}
		// Resume from the last row read, not the last row kept: the rows in
		// between were skipped on purpose and must not be re-read.
		last := rows[len(rows)-1]
		cursorName = pgtype.Text{String: strings.ToLower(last.Name), Valid: true}
		cursorID = uuid.NullUUID{UUID: last.ID, Valid: true}

		if scanned >= maxAgentRowsScanned {
			return nil, oops.E(oops.CodeUnexpected, nil,
				"this organization has more agents than one page can be assembled from; narrow the list with a search or a filter")
		}
	}

	// Only a row the caller has been shown may be named by the cursor. It is
	// base64, not a sealed token, so naming a row they could not read would
	// hand them that agent's name and id.
	var nextCursor *string
	if len(readable) > limit {
		readable = readable[:limit]
		last := readable[len(readable)-1]
		value := encodeAgentCursor(last.Name, last.ID)
		nextCursor = &value
	}

	// Fetch profiles only for readable agents, scoped to this request's tenant.
	ownerProfiles := make(map[string]*gen.AgentOwnerProfile, len(ownerIDs))
	if len(ownerIDs) > 0 {
		profiles, err := queries.ListAgentOwnerProfiles(ctx, repo.ListAgentOwnerProfilesParams{
			OrganizationID: human.Auth.ActiveOrganizationID, OwnerUserIds: ownerIDs,
		})
		if err != nil {
			return nil, s.serviceError(ctx, err, "load agent owner profiles")
		}
		for _, profile := range profiles {
			view := &gen.AgentOwnerProfile{DisplayName: profile.DisplayName, PhotoURL: nil}
			if profile.PhotoUrl.Valid {
				view.PhotoURL = &profile.PhotoUrl.String
			}
			ownerProfiles[profile.ID] = view
		}
	}

	items := make([]*gen.ManagedAgent, 0, len(readable))
	for _, agent := range readable {
		items = append(items, managedAgentView(agent, permissionsByID[agent.ID.String()], ownerProfiles[agent.OwnerUserID]))
	}
	return &gen.ListAgentsResult{Items: items, NextCursor: nextCursor}, nil
}

// Goa validates the format; this turns it into the nullable column value the
// query compares against.
func optionalMoment(
	payload *gen.ListPayload,
	read func(*gen.ListPayload) *string,
) (pgtype.Timestamptz, error) {
	absent := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if payload == nil {
		return absent, nil
	}
	value := read(payload)
	if value == nil || *value == "" {
		return absent, nil
	}
	moment, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return absent, fmt.Errorf("parse moment: %w", err)
	}
	return pgtype.Timestamptz{Time: moment, InfinityModifier: pgtype.Finite, Valid: true}, nil
}
