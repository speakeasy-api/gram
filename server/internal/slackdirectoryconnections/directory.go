package slackdirectoryconnections

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
)

func (s *Service) Sync(ctx context.Context, p *gen.SyncPayload) (*gen.SyncResult, error) {
	ac, err := s.authorizeMutation(ctx)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	generation, err := uuid.Parse(p.Generation)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	row, err := repo.New(s.db).GetSlackDirectoryConnection(ctx, repo.GetSlackDirectoryConnectionParams{OrganizationID: ac.ActiveOrganizationID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not read Slack workspace").LogError(ctx, s.logger)
	}
	if row.Generation != generation {
		return nil, oops.E(oops.CodeConflict, nil, "The workspace connection changed. Reload and try again.")
	}
	if _, err := usableTokens(s.encryption, row); err != nil {
		return nil, oops.E(oops.CodeConflict, nil, "Reconnect this Slack workspace before syncing.")
	}
	if err := s.syncScheduler.Start(ctx, SyncInput{OrganizationID: ac.ActiveOrganizationID, ConnectionID: id, Generation: generation, ActorID: ac.UserID, StartedAt: time.Time{}}); err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "Could not start the sync. Try again.").LogError(ctx, s.logger)
	}
	return &gen.SyncResult{Accepted: true}, nil
}

func (s *Service) ListMembers(ctx context.Context, p *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	ac, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	connectionID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if p.ConnectionID != nil {
		id, err := uuid.Parse(*p.ConnectionID)
		if err != nil {
			return nil, oops.C(oops.CodeBadRequest)
		}
		connectionID = uuid.NullUUID{UUID: id, Valid: true}
		if _, err := repo.New(s.db).GetSlackDirectoryConnection(ctx, repo.GetSlackDirectoryConnectionParams{OrganizationID: ac.ActiveOrganizationID, ID: id}); errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		} else if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "could not read Slack workspace").LogError(ctx, s.logger)
		}
	}
	mappingStatus := conv.PtrValOr(p.MappingStatus, "")
	if mappingStatus != "" && mappingStatus != "mapped" && mappingStatus != "unmapped" && mappingStatus != "needs_review" {
		return nil, oops.C(oops.CodeBadRequest)
	}
	search := strings.TrimSpace(conv.PtrValOr(p.Search, ""))
	limit := p.Limit
	if limit == 0 {
		limit = 50
	}
	page := p.Page
	if page == 0 {
		page = 1
	}
	if limit < 1 || limit > 100 || page < 1 || page > math.MaxInt32/limit || utf8.RuneCountInString(search) > 200 {
		return nil, oops.C(oops.CodeBadRequest)
	}
	sortAsOf := time.Now()
	if p.SortAsOf != nil {
		parsed, err := time.Parse(time.RFC3339, *p.SortAsOf)
		if err != nil {
			return nil, oops.C(oops.CodeBadRequest)
		}
		sortAsOf = parsed
	}
	offset := int32((page - 1) * limit) // #nosec G115 -- page is bounded above so the offset fits in int32.
	q := repo.New(s.db)
	rows, err := q.ListSlackDirectoryMembersPage(ctx, repo.ListSlackDirectoryMembersPageParams{OrganizationID: ac.ActiveOrganizationID, ConnectionID: connectionID, MappingStatus: mappingStatus, Search: search, IncludeDeactivated: p.IncludeDeactivated, IncludeBots: p.IncludeBots, IncludeGuests: p.IncludeGuests, SortAsOf: pgtype.Timestamptz{Time: sortAsOf, Valid: true, InfinityModifier: pgtype.Finite}, PageOffset: offset, PageSize: int32(limit)})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not list Slack members").LogError(ctx, s.logger)
	}
	total, err := q.CountSlackDirectoryMembers(ctx, repo.CountSlackDirectoryMembersParams{OrganizationID: ac.ActiveOrganizationID, ConnectionID: connectionID, Search: search, MappingStatus: mappingStatus, IncludeDeactivated: p.IncludeDeactivated, IncludeBots: p.IncludeBots, IncludeGuests: p.IncludeGuests})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not count Slack members").LogError(ctx, s.logger)
	}
	result := &gen.ListMembersResult{Members: make([]*gen.SlackDirectoryMember, 0, len(rows)), Total: total}
	for _, row := range rows {
		result.Members = append(result.Members, mv.BuildSlackDirectoryMemberView(repo.ListSlackDirectoryMembersRow(row)))
	}
	return result, nil
}
