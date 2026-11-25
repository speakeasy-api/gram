package admin

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Validate here as well as in Goa: staff MCP calls the service directly.
func userPagination(page, limit *int) (int, int32, int32, error) {
	p, l := 1, 50
	if page != nil {
		p = *page
	}
	if limit != nil {
		l = *limit
	}
	if p < 1 || l < 1 || l > 100 || p-1 > math.MaxInt32/l {
		return 0, 0, 0, oops.E(oops.CodeInvalid, nil, "page must be positive, limit must be 1–100, and offset must fit int32")
	}
	offset := (p - 1) * l
	if offset < 0 || offset > math.MaxInt32 {
		return 0, 0, 0, oops.E(oops.CodeInvalid, nil, "offset must fit int32")
	}
	return p, int32(l), int32(offset), nil
}

func userTimestamp(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	v := t.Time.Format(time.RFC3339)
	return &v
}

func (s *Service) ListUsers(ctx context.Context, payload *gen.ListUsersPayload) (*gen.AdminListUsersResult, error) {
	page, limit, offset, err := userPagination(payload.Page, payload.Limit)
	if err != nil {
		return nil, err
	}
	query := ""
	if payload.Q != nil {
		query = *payload.Q
	}
	terms, err := ParseUserSearch(query)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, nil, "%s", err.Error())
	}
	params := repo.AdminListUsersParams{PageLimit: limit, PageOffset: offset, NamePatterns: []string{}, EmailPatterns: []string{}, OrgPatterns: []string{}, AnyPatterns: []string{}}
	escape := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	for _, term := range terms {
		pattern := "%" + escape.Replace(term.Value) + "%"
		switch term.Field {
		case "name":
			params.NamePatterns = append(params.NamePatterns, pattern)
		case "email":
			params.EmailPatterns = append(params.EmailPatterns, pattern)
		case "org":
			params.OrgPatterns = append(params.OrgPatterns, pattern)
		default:
			params.AnyPatterns = append(params.AnyPatterns, pattern)
		}
	}
	q := repo.New(s.db)
	total, err := q.AdminCountUsers(ctx, repo.AdminCountUsersParams{NamePatterns: params.NamePatterns, EmailPatterns: params.EmailPatterns, OrgPatterns: params.OrgPatterns, AnyPatterns: params.AnyPatterns})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count users").LogError(ctx, s.logger)
	}
	rows, err := q.AdminListUsers(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list users").LogError(ctx, s.logger)
	}
	users := make([]*gen.AdminUser, 0, len(rows))
	ids := make([]string, 0, len(rows))
	byID := make(map[string]*gen.AdminUser, len(rows))
	for _, row := range rows {
		user := &gen.AdminUser{ID: row.ID, Email: row.Email, DisplayName: row.DisplayName, LastLogin: userTimestamp(row.LastLogin), Organizations: []*gen.AdminUserOrganization{}, OrganizationCount: 0}
		users = append(users, user)
		ids = append(ids, row.ID)
		byID[row.ID] = user
	}
	if len(ids) > 0 {
		previews, err := q.AdminListUsersOrganizationPreviews(ctx, ids)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list user organization previews").LogError(ctx, s.logger)
		}
		for _, row := range previews {
			user := byID[row.UserID.String]
			user.OrganizationCount = row.OrganizationCount
			user.Organizations = append(user.Organizations, &gen.AdminUserOrganization{ID: row.ID, Name: row.Name, Slug: row.Slug, DisabledAt: userTimestamp(row.DisabledAt)})
		}
	}
	return &gen.AdminListUsersResult{Users: users, Total: total, Page: page, Limit: int(limit)}, nil
}

func (s *Service) ListUserOrganizations(ctx context.Context, payload *gen.ListUserOrganizationsPayload) (*gen.AdminListUserOrganizationsResult, error) {
	page, limit, offset, err := userPagination(payload.Page, payload.Limit)
	if err != nil {
		return nil, err
	}
	if payload.UserID == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "user_id is required")
	}
	q := repo.New(s.db)
	total, err := q.AdminCountUserOrganizations(ctx, payload.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, nil, "user not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count user organizations").LogError(ctx, s.logger)
	}
	rows, err := q.AdminListUserOrganizations(ctx, repo.AdminListUserOrganizationsParams{UserID: payload.UserID, PageLimit: limit, PageOffset: offset})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user organizations").LogError(ctx, s.logger)
	}
	organizations := make([]*gen.AdminUserOrganization, 0, len(rows))
	for _, row := range rows {
		organizations = append(organizations, &gen.AdminUserOrganization{ID: row.ID, Name: row.Name, Slug: row.Slug, DisabledAt: userTimestamp(row.DisabledAt)})
	}
	return &gen.AdminListUserOrganizationsResult{Organizations: organizations, Total: total, Page: page, Limit: int(limit)}, nil
}
