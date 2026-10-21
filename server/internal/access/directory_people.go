package access

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	defaultDirectoryPeopleLimit = 500
	maxDirectoryPeopleLimit     = 1000
)

// ListDirectoryPeople returns the organization's live directory profiles with
// one state each, so the dashboard can show people the directory knows about
// even before they sign in. It is session-only (API keys cannot enumerate it)
// and never returns attribute values, which stay behind org:admin on the
// mapping reads.
func (s *Service) ListDirectoryPeople(ctx context.Context, payload *gen.ListDirectoryPeoplePayload) (*gen.ListDirectoryPeopleResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	limit := defaultDirectoryPeopleLimit
	if payload != nil && payload.Limit > 0 {
		limit = min(payload.Limit, maxDirectoryPeopleLimit)
	}
	cursor := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload != nil && payload.Cursor != nil && *payload.Cursor != "" {
		parsed, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
		cursor = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	// One extra row says whether another page exists.
	rows, err := directoryrepo.New(s.db).ListDirectoryPeople(ctx, directoryrepo.ListDirectoryPeopleParams{
		OrganizationID: ac.ActiveOrganizationID,
		Cursor:         cursor,
		LimitValue:     int32(limit + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list directory people").LogError(ctx, s.logger)
	}

	result := &gen.ListDirectoryPeopleResult{
		People:     make([]*gen.DirectoryPerson, 0, min(len(rows), limit)),
		Counts:     &gen.DirectoryPeopleStateCounts{Member: 0, AccountNotMember: 0, NotSignedIn: 0, AmbiguousEmail: 0},
		NextCursor: nil,
	}
	if len(rows) > 0 {
		// Every row carries the same organization-wide totals.
		result.Counts = &gen.DirectoryPeopleStateCounts{
			Member:           rows[0].MemberCount,
			AccountNotMember: rows[0].AccountNotMemberCount,
			NotSignedIn:      rows[0].NotSignedInCount,
			AmbiguousEmail:   rows[0].AmbiguousEmailCount,
		}
	}
	if len(rows) > limit {
		rows = rows[:limit]
		next := rows[len(rows)-1].ID.String()
		result.NextCursor = &next
	}
	for _, row := range rows {
		result.People = append(result.People, directoryPersonFromRow(row))
	}
	return result, nil
}

func directoryPersonFromRow(row directoryrepo.ListDirectoryPeopleRow) *gen.DirectoryPerson {
	groups := make([]*gen.DirectoryPersonGroup, 0, len(row.GroupIds))
	for i, id := range row.GroupIds {
		if i >= len(row.GroupNames) {
			break
		}
		groups = append(groups, &gen.DirectoryPersonGroup{ID: id.String(), Name: row.GroupNames[i]})
	}
	var userID *string
	if row.AccountUserID != "" {
		userID = &row.AccountUserID
	}
	return &gen.DirectoryPerson{
		ID:        row.ID.String(),
		Email:     row.Email.String,
		FirstName: nonEmptyName(row.FirstName),
		LastName:  nonEmptyName(row.LastName),
		State:     row.State,
		UserID:    userID,
		Groups:    groups,
	}
}

func nonEmptyName(value pgtype.Text) *string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	return &value.String
}
