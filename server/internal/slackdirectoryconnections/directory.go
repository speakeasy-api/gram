package slackdirectoryconnections

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
	cursor := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
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
	if p.Cursor != nil {
		id, err := uuid.Parse(*p.Cursor)
		if err != nil {
			return nil, oops.C(oops.CodeBadRequest)
		}
		cursor = uuid.NullUUID{UUID: id, Valid: true}
	}
	search := strings.TrimSpace(conv.PtrValOr(p.Search, ""))
	limit := p.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || utf8.RuneCountInString(search) > 200 {
		return nil, oops.C(oops.CodeBadRequest)
	}
	rows, err := repo.New(s.db).ListSlackDirectoryMembers(ctx, repo.ListSlackDirectoryMembersParams{OrganizationID: ac.ActiveOrganizationID, ConnectionID: connectionID, Cursor: cursor, Search: search, PageSize: int32(limit + 1)})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not list Slack members").LogError(ctx, s.logger)
	}
	total, err := repo.New(s.db).CountSlackDirectoryMembers(ctx, repo.CountSlackDirectoryMembersParams{OrganizationID: ac.ActiveOrganizationID, ConnectionID: connectionID, Search: search})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not count Slack members").LogError(ctx, s.logger)
	}
	result := &gen.ListMembersResult{Members: make([]*gen.SlackDirectoryMember, 0, min(len(rows), limit)), Total: total, NextCursor: nil}
	if len(rows) > limit {
		result.NextCursor = conv.PtrEmpty(rows[limit-1].ID.String())
		rows = rows[:limit]
	}
	for _, row := range rows {
		result.Members = append(result.Members, &gen.SlackDirectoryMember{ID: row.ID.String(), ConnectionID: row.ConnectionID.String(), WorkspaceID: row.SlackTeamID, WorkspaceName: row.WorkspaceName.String, SlackUserID: row.SlackUserID, DisplayName: conv.FromPGText[string](row.DisplayName), Email: conv.FromPGText[string](row.Email), Status: row.Status, MemberType: row.MemberType, LastSeenAt: row.LastSeenAt.Time.Format(time.RFC3339), ObservedInLastSync: row.ObservedInLastSync})
	}
	return result, nil
}
