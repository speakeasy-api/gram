package platformmcp

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// maxWhoamiOtherOrganizations bounds the switch list. A staff account can
// belong to many organizations; the agent only needs enough to name a few.
const maxWhoamiOtherOrganizations = 50

// WhoamiUser carries no user ID: no tool takes one, and the email already
// identifies the caller.
type WhoamiUser struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

type WhoamiOrganization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type WhoamiRole struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type WhoamiOutput struct {
	User         WhoamiUser         `json:"user"`
	Organization WhoamiOrganization `json:"organization"`
	// Roles are the caller's live roles in the active organization, from
	// direct assignments and directory role mappings alike.
	Roles []WhoamiRole `json:"roles"`
	// ClientName is the name the connecting OAuth client registered. The client
	// chooses it, so it identifies the connection but proves nothing.
	ClientName string `json:"client_name,omitempty"`
	// OtherOrganizations are the caller's other active memberships, which they
	// can reconnect to. Empty on the project assistant: it has no connection
	// to switch, and its transcript belongs to the active organization.
	OtherOrganizations          []WhoamiOrganization `json:"other_organizations"`
	OtherOrganizationsTruncated bool                 `json:"other_organizations_truncated"`
}

// WhoamiService backs the whoami tool.
type WhoamiService struct {
	db *pgxpool.Pool
}

func NewWhoamiService(db *pgxpool.Pool) *WhoamiService {
	return &WhoamiService{db: db}
}

func (s *WhoamiService) Whoami(ctx context.Context, principal Principal) (WhoamiOutput, error) {
	if s == nil || s.db == nil || principal.UserID == "" || principal.OrganizationID == "" {
		return WhoamiOutput{}, ErrUnavailable
	}

	user, err := usersrepo.New(s.db).GetUser(ctx, principal.UserID)
	if err != nil {
		return WhoamiOutput{}, fmt.Errorf("get whoami user: %w", err)
	}
	organization, err := organizationsrepo.New(s.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return WhoamiOutput{}, fmt.Errorf("get whoami organization: %w", err)
	}
	roles, err := s.roles(ctx, principal)
	if err != nil {
		return WhoamiOutput{}, err
	}

	output := WhoamiOutput{
		User:                        WhoamiUser{Email: user.Email, DisplayName: user.DisplayName},
		Organization:                WhoamiOrganization{ID: organization.ID, Name: organization.Name, Slug: organization.Slug},
		Roles:                       roles,
		ClientName:                  "",
		OtherOrganizations:          []WhoamiOrganization{},
		OtherOrganizationsTruncated: false,
	}
	if principal.surface() != SurfacePlatformMCP {
		return output, nil
	}

	if principal.ClientID != "" {
		client, err := platformrepo.New(s.db).GetActivePlatformMCPOAuthClientByClientID(ctx, principal.ClientID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return WhoamiOutput{}, fmt.Errorf("get whoami oauth client: %w", err)
		default:
			output.ClientName = client.ClientName
		}
	}

	memberships, err := organizationsrepo.New(s.db).ListOrganizationsForUser(ctx, pgtype.Text{String: principal.UserID, Valid: true})
	if err != nil {
		return WhoamiOutput{}, fmt.Errorf("list whoami organizations: %w", err)
	}
	for _, membership := range memberships {
		if membership.ID == principal.OrganizationID {
			continue
		}
		output.OtherOrganizations = append(output.OtherOrganizations, WhoamiOrganization{ID: membership.ID, Name: membership.Name, Slug: membership.Slug})
	}
	slices.SortFunc(output.OtherOrganizations, func(a, b WhoamiOrganization) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	if len(output.OtherOrganizations) > maxWhoamiOtherOrganizations {
		output.OtherOrganizations = output.OtherOrganizations[:maxWhoamiOtherOrganizations]
		output.OtherOrganizationsTruncated = true
	}
	return output, nil
}

// roles resolves role principals the same way authorization does, so the
// answer matches what the caller can actually do.
func (s *WhoamiService) roles(ctx context.Context, principal Principal) ([]WhoamiRole, error) {
	principals, err := authz.ResolveUserPrincipals(ctx, s.db, principal.OrganizationID, principal.UserID)
	if err != nil {
		return nil, fmt.Errorf("resolve whoami principals: %w", err)
	}
	rows, err := accessrepo.New(s.db).ListActiveOrganizationRoles(ctx, principal.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list whoami roles: %w", err)
	}
	byURN := make(map[string]WhoamiRole, len(rows))
	for _, row := range rows {
		byURN[row.RoleUrn] = WhoamiRole{Name: row.WorkosName, Slug: row.WorkosSlug}
	}
	roles := []WhoamiRole{}
	for _, p := range principals {
		if role, ok := byURN[p.String()]; ok {
			roles = append(roles, role)
		}
	}
	slices.SortFunc(roles, func(a, b WhoamiRole) int { return cmp.Compare(a.Name, b.Name) })
	return slices.Compact(roles), nil
}
