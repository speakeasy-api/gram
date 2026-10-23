package telemetry

import (
	"cmp"
	"context"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// authorizerUserIDsForUserFilters resolves the user filters to the user ids an
// agent records as its authorizer, so picking a person also selects the calls
// agents made on their behalf. A user id filter is that id already. An email
// resolves to its owner through the identity rules: the directory owner wins,
// a linked account email resolves to its single owner, and an address several
// people share resolves to nobody, so a filter never selects someone else's
// delegated calls.
//
// Every email resolves in one batch, so a filter built from a large role costs
// a fixed number of queries. Best effort, like the resolver: a failed lookup
// leaves the emails matching only the calls recorded under the addresses
// themselves.
func (s *Service) authorizerUserIDsForUserFilters(ctx context.Context, orgID string, filters []repo.ToolUsageUserFilter) []string {
	if orgID == "" {
		return nil
	}

	ids := make([]string, 0, len(filters))
	emails := make([]string, 0, len(filters))
	for _, filter := range filters {
		switch filter.Kind {
		case "user_id":
			ids = append(ids, filter.Key)
		case "email":
			emails = append(emails, filter.Key)
		}
	}
	// Walk the filters rather than the map, so the same filter always builds
	// the same id list.
	owners := s.identities.EmailOwnerUserIDs(ctx, orgID, emails)
	for _, email := range emails {
		ids = append(ids, owners[conv.NormalizeEmail(email)])
	}
	return conv.DedupeNonEmpty(ids)
}

// authorizerLabels names the people the listed agent calls acted for.
func (s *Service) authorizerLabels(ctx context.Context, orgID string, rows []repo.ToolUsageTraceSummary) map[string]string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.AuthorizerUserID)
	}
	return s.authorizerEmails(ctx, orgID, ids)
}

// authorizerUserOptions adds the people agents acted for to the User filter
// options, so someone whose only calls in the window were made on their behalf
// can still be picked. They are offered by email, the key the other user
// options and the user filter already use, and fold into a person's direct
// option when they have one. The count then covers both, as picking the
// person selects both.
func (s *Service) authorizerUserOptions(ctx context.Context, orgID string, users []repo.ToolUsageUserFilterOptionRow, authorizers []repo.ToolUsageAuthorizerFilterOptionRow) []repo.ToolUsageUserFilterOptionRow {
	ids := make([]string, 0, len(authorizers))
	for _, row := range authorizers {
		ids = append(ids, row.AuthorizerUserID)
	}
	emails := s.authorizerEmails(ctx, orgID, ids)
	if len(emails) == 0 {
		return users
	}

	byEmail := make(map[string]int, len(users))
	for i, user := range users {
		if user.UserKind == "email" {
			byEmail[conv.NormalizeEmail(user.UserKey)] = i
		}
	}
	for _, row := range authorizers {
		email, ok := emails[row.AuthorizerUserID]
		if !ok {
			continue
		}
		key := conv.NormalizeEmail(email)
		if i, ok := byEmail[key]; ok {
			users[i].EventCount += row.EventCount
			continue
		}
		byEmail[key] = len(users)
		users = append(users, repo.ToolUsageUserFilterOptionRow{
			UserKey:    email,
			UserLabel:  email,
			UserKind:   "email",
			EventCount: row.EventCount,
		})
	}

	// The merge changes counts, so restore the user options query's order:
	// most active first, then by name.
	slices.SortFunc(users, func(a, b repo.ToolUsageUserFilterOptionRow) int {
		return cmp.Or(cmp.Compare(b.EventCount, a.EventCount), cmp.Compare(a.UserLabel, b.UserLabel))
	})
	return users
}

// authorizerEmails resolves the user ids agents recorded as their authorizer
// to emails. Scoped to the organization, so an id recorded on a row never
// resolves against another tenant's directory. A failed lookup resolves
// nobody rather than failing the page: the id still identifies the person.
func (s *Service) authorizerEmails(ctx context.Context, orgID string, ids []string) map[string]string {
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
		// GetConnectedUsersByIDs filters removed memberships but not
		// soft-deleted users, who must not label rows any more than the
		// filter resolves to them.
		if user.DeletedAt.Valid {
			continue
		}
		labels[user.ID] = user.Email
	}
	return labels
}
