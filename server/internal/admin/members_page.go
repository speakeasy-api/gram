package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// MaxMemberPageSize bounds a staff member lookup independently of the dashboard's capped roster.
const MaxMemberPageSize = 50

// MemberPage is a canonical-organisation roster page ordered by stable user ID.
type MemberPage struct {
	// OrganizationID is the exact organisation that supplied every member.
	OrganizationID string

	// Members contains only active organisation relationships.
	Members []*gen.AdminOrganizationMember

	// NextUserID is the last returned user when another page exists.
	NextUserID *string
}

// ListOrganizationMembersPage returns a bounded exact-organisation page without changing the dashboard's roster contract.
func (s *Service) ListOrganizationMembersPage(ctx context.Context, organizationID, afterUserID string, limit int) (*MemberPage, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if organizationID == "" || limit < 1 || limit > MaxMemberPageSize {
		return nil, oops.E(oops.CodeBadRequest, nil, "provide an exact organization and a bounded member page")
	}
	queries := repo.New(s.db)
	if _, err := queries.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: organizationID, AllowSlug: false}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, fmt.Errorf("resolve member organization: %w", err)
	}
	if afterUserID != "" {
		if _, err := queries.AdminGetOrganizationMemberCursor(ctx, repo.AdminGetOrganizationMemberCursorParams{OrganizationID: organizationID, UserID: afterUserID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, nil, "member cursor is unavailable for this organization")
			}
			return nil, fmt.Errorf("resolve member cursor: %w", err)
		}
	}
	rows, err := queries.AdminListOrganizationMembersPage(ctx, repo.AdminListOrganizationMembersPageParams{OrganizationID: organizationID, AfterUserID: afterUserID, PageLimit: conv.SafeInt32(limit + 1)})
	if err != nil {
		return nil, fmt.Errorf("list organization member page: %w", err)
	}
	page := &MemberPage{OrganizationID: organizationID, Members: make([]*gen.AdminOrganizationMember, 0, min(limit, len(rows))), NextUserID: nil}
	if len(rows) > limit {
		page.NextUserID = new(rows[limit-1].ID)
		rows = rows[:limit]
	}
	for _, row := range rows {
		var lastLogin *string
		if row.LastLogin.Valid {
			lastLogin = new(row.LastLogin.Time.UTC().Format(time.RFC3339))
		}
		page.Members = append(page.Members, &gen.AdminOrganizationMember{
			ID: row.ID, Email: row.Email, DisplayName: row.DisplayName, LastLogin: lastLogin,
			CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339), UpdatedAt: row.UpdatedAt.Time.UTC().Format(time.RFC3339),
		})
	}
	return page, nil
}
