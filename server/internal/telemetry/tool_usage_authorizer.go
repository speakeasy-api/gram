package telemetry

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// authorizerUserIDsForEmailFilters resolves the email user filters to the user
// ids an agent records as its authorizer, so picking a person also selects the
// calls agents made on their behalf. Linked account emails resolve to their
// owner too, the way the summary resolves them elsewhere.
//
// A failed lookup is an error rather than an empty result: dropping the ids
// would quietly hide every delegated call from a filter that asked for them.
func (s *Service) authorizerUserIDsForEmailFilters(ctx context.Context, orgID string, filters []repo.ToolUsageUserFilter) ([]string, error) {
	emails := make([]string, 0, len(filters))
	for _, filter := range filters {
		if filter.Kind == "email" {
			emails = append(emails, filter.Key)
		}
	}
	emails = conv.DedupeNonEmpty(emails)
	if len(emails) == 0 || orgID == "" {
		return nil, nil
	}

	users, err := usersRepo.New(s.db).GetConnectedUsersByEmails(ctx, usersRepo.GetConnectedUsersByEmailsParams{
		Emails:         emails,
		OrganizationID: orgID,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve user filter emails to users: %w", err)
	}
	accounts, err := s.hooksRepo.ListUserAccountsByEmails(ctx, hooksRepo.ListUserAccountsByEmailsParams{
		OrganizationID: orgID,
		Emails:         emails,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve user filter emails to linked accounts: %w", err)
	}

	ids := make([]string, 0, len(users)+len(accounts))
	for _, user := range users {
		ids = append(ids, user.ID)
	}
	for _, account := range accounts {
		if account.UserID.Valid {
			ids = append(ids, account.UserID.String)
		}
	}
	return conv.DedupeNonEmpty(ids), nil
}

// authorizerLabels names the people the listed agent calls acted for. Scoped to
// the organization, so an id recorded on a row never resolves against another
// tenant's directory. A failed lookup leaves the rows unlabelled rather than
// failing the page: the id still identifies the person.
func (s *Service) authorizerLabels(ctx context.Context, orgID string, rows []repo.ToolUsageTraceSummary) map[string]string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.AuthorizerUserID)
	}
	ids = conv.DedupeNonEmpty(ids)
	if len(ids) == 0 || orgID == "" {
		return nil
	}

	users, err := usersRepo.New(s.db).GetConnectedUsersByIDs(ctx, usersRepo.GetConnectedUsersByIDsParams{
		Ids:            ids,
		OrganizationID: orgID,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to resolve tool usage authorizers", attr.SlogError(err))
		return nil
	}
	labels := make(map[string]string, len(users))
	for _, user := range users {
		labels[user.ID] = user.Email
	}
	return labels
}
