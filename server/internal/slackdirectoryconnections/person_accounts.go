package slackdirectoryconnections

import (
	"context"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
)

func (s *Service) ListPersonAccounts(ctx context.Context, p *gen.ListPersonAccountsPayload) (*gen.ListPersonAccountsResult, error) {
	ac, err := s.authorizePerson(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	if p.UserID == "" {
		return nil, oops.C(oops.CodeBadRequest)
	}
	// Check live membership even when a session or cached admin grant survives removal.
	people := orgrepo.New(s.db)
	for _, userID := range []string{ac.UserID, p.UserID} {
		active, err := people.HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{OrganizationID: ac.ActiveOrganizationID, UserID: userID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "could not check organization membership").LogError(ctx, s.logger)
		}
		if !active {
			return nil, oops.C(oops.CodeNotFound)
		}
	}
	cursor := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if p.Cursor != nil {
		id, err := uuid.Parse(*p.Cursor)
		if err != nil {
			return nil, oops.C(oops.CodeBadRequest)
		}
		cursor = uuid.NullUUID{UUID: id, Valid: true}
	}
	const pageSize = 50
	rows, err := repo.New(s.db).ListSlackDirectoryMembers(ctx, repo.ListSlackDirectoryMembersParams{
		OrganizationID: ac.ActiveOrganizationID, MappedUserID: p.UserID,
		ConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, MemberID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Cursor: cursor, MappingStatus: "", Search: "", PageSize: pageSize + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not read mapped Slack accounts").LogError(ctx, s.logger)
	}
	result := &gen.ListPersonAccountsResult{Accounts: make([]*gen.SlackPersonAccount, 0, min(len(rows), pageSize)), NextCursor: nil}
	if len(rows) > pageSize {
		result.NextCursor = conv.PtrEmpty(rows[pageSize-1].ID.String())
		rows = rows[:pageSize]
	}
	if len(rows) == 0 {
		return result, nil
	}
	connections, err := repo.New(s.db).ListSlackDirectoryConnections(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not read Slack workspace freshness").LogError(ctx, s.logger)
	}
	byID := make(map[uuid.UUID]repo.SlackDirectoryConnection, len(connections))
	for _, connection := range connections {
		byID[connection.ID] = connection
	}
	views := make(map[uuid.UUID]*gen.SlackDirectoryConnection)
	for _, row := range rows {
		connection := views[row.ConnectionID]
		if connection == nil {
			stored, ok := byID[row.ConnectionID]
			if !ok {
				return nil, oops.C(oops.CodeNotFound)
			}
			connection = s.connectionView(ctx, stored)
			views[row.ConnectionID] = connection
		}
		result.Accounts = append(result.Accounts, &gen.SlackPersonAccount{
			Member: mv.BuildSlackDirectoryMemberView(row), DirectoryStatus: connection.DirectoryStatus,
			LastFullSyncSucceededAt: connection.LastFullSyncSucceededAt,
		})
	}
	return result, nil
}
