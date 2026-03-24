package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *Service) authContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil {
		return nil, gen.MakeUnauthorized(errors.New("missing auth context"))
	}
	return ac, nil
}

// workosOrgID resolves the WorkOS organization ID from the Gram org ID stored
// in the auth context.
func (s *Service) workosOrgID(ctx context.Context, gramOrgID string) (string, error) {
	q := orgrepo.New(s.db)
	org, err := q.GetOrganizationMetadata(ctx, gramOrgID)
	if err != nil {
		return "", fmt.Errorf("get organization metadata: %w", err)
	}
	if !org.WorkosID.Valid || org.WorkosID.String == "" {
		return "", gen.MakeBadRequest(errors.New("organization is not linked to WorkOS"))
	}
	return org.WorkosID.String, nil
}

// systemRoleSlugs are the WorkOS environment roles that get default grants on first access.
var systemRoleSlugs = []string{"admin", "member"}

// defaultGrants defines the seed grants for each system role.
var defaultGrants = map[string][]string{
	"admin":  {"org:read", "org:admin", "build:read", "build:write", "mcp:read", "mcp:write", "mcp:connect"},
	"member": {"org:read", "build:read", "mcp:read", "mcp:connect"},
}

// ensureDefaultGrants lazily seeds the default principal_grants for system
// roles (admin, member) the first time an org uses RBAC. If any grants
// already exist for the org we skip seeding entirely.
func (s *Service) ensureDefaultGrants(ctx context.Context, orgID string) {
	q := repo.New(s.db)

	// Check if any grants exist for this org (empty PrincipalUrn = no filter).
	existing, err := q.ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: orgID,
		PrincipalUrn:   "",
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to check existing grants for default seeding",
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
		return
	}
	if len(existing) > 0 {
		return
	}

	// Build seed params for all system roles.
	var seeds []repo.SeedOrgRoleGrantsParams
	for _, slug := range systemRoleSlugs {
		principal := urn.NewPrincipal(urn.PrincipalTypeRole, slug)
		for _, scope := range defaultGrants[slug] {
			seeds = append(seeds, repo.SeedOrgRoleGrantsParams{
				OrganizationID: orgID,
				PrincipalUrn:   principal,
				Scope:          scope,
				Resource:       "*",
			})
		}
	}

	if _, err := q.SeedOrgRoleGrants(ctx, seeds); err != nil {
		s.logger.WarnContext(ctx, "failed to seed default grants for system roles",
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
		return
	}

	s.logger.InfoContext(ctx, "seeded default grants for system roles",
		attr.SlogOrganizationID(orgID),
		attr.SlogValueInt(len(seeds)),
	)
}

// grantsForRole loads principal_grants for a role slug (principal_urn = "role:<slug>").
func (s *Service) grantsForRole(ctx context.Context, orgID string, roleSlug string) ([]*gen.RoleGrant, error) {
	q := repo.New(s.db)
	principalURN := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)
	rows, err := q.ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: orgID,
		PrincipalUrn:   principalURN.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list grants for role %q: %w", roleSlug, err)
	}

	// Group grant rows by scope → resources.
	// A resource of "*" means unrestricted (nil in the API).
	type scopeAgg struct {
		unrestricted bool
		resources    []string
	}
	byScope := make(map[string]*scopeAgg)
	for _, row := range rows {
		agg, ok := byScope[row.Scope]
		if !ok {
			agg = &scopeAgg{}
			byScope[row.Scope] = agg
		}
		if row.Resource == "*" {
			agg.unrestricted = true
		} else {
			agg.resources = append(agg.resources, row.Resource)
		}
	}

	grants := make([]*gen.RoleGrant, 0, len(byScope))
	for scope, agg := range byScope {
		g := &gen.RoleGrant{Scope: scope}
		if !agg.unrestricted {
			g.Resources = agg.resources
		}
		grants = append(grants, g)
	}

	return grants, nil
}

// syncGrants replaces all principal_grants for a role with the provided grants.
// The delete-then-insert is wrapped in a transaction so a partial failure does
// not leave the role with missing grants.
func (s *Service) syncGrants(ctx context.Context, orgID string, roleSlug string, grants []*gen.RoleGrant) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := repo.New(tx)
	principalURN := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)

	// Delete existing grants for this role.
	if _, err := q.DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: orgID,
		PrincipalUrn:   principalURN,
	}); err != nil {
		return fmt.Errorf("delete existing grants: %w", err)
	}

	// Insert new grants.
	for _, g := range grants {
		if len(g.Resources) == 0 {
			// Unrestricted grant.
			if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
				OrganizationID: orgID,
				PrincipalUrn:   principalURN,
				Scope:          g.Scope,
				Resource:       "*",
			}); err != nil {
				return fmt.Errorf("upsert unrestricted grant %q: %w", g.Scope, err)
			}
		} else {
			for _, res := range g.Resources {
				if _, err := q.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
					OrganizationID: orgID,
					PrincipalUrn:   principalURN,
					Scope:          g.Scope,
					Resource:       res,
				}); err != nil {
					return fmt.Errorf("upsert grant %q resource %q: %w", g.Scope, res, err)
				}
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Service methods
// ---------------------------------------------------------------------------

func (s *Service) ListRoles(ctx context.Context, _ *gen.ListRolesPayload) (*gen.ListRolesResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	// Lazily seed default grants for system roles on first access.
	s.ensureDefaultGrants(ctx, ac.ActiveOrganizationID)

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("list roles from workos: %w", err))
	}

	// Count members per role.
	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("list members from workos: %w", err))
	}
	memberCounts := make(map[string]int)
	for _, m := range members {
		memberCounts[m.Role.Slug]++
	}

	result := &gen.ListRolesResult{
		Roles: make([]*gen.Role, 0, len(wRoles)),
	}
	for _, wr := range wRoles {
		grants, err := s.grantsForRole(ctx, ac.ActiveOrganizationID, wr.Slug)
		if err != nil {
			return nil, gen.MakeUnexpected(err)
		}

		result.Roles = append(result.Roles, &gen.Role{
			ID:          wr.ID,
			Name:        wr.Name,
			Description: wr.Description,
			IsSystem:    wr.Type == "EnvironmentRole",
			Grants:      grants,
			MemberCount: memberCounts[wr.Slug],
			CreatedAt:   wr.CreatedAt,
			UpdatedAt:   wr.UpdatedAt,
		})
	}

	return result, nil
}

func (s *Service) GetRole(ctx context.Context, p *gen.GetRolePayload) (*gen.Role, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	// WorkOS ListOrganizationRoles is the only way to get roles; find ours by ID.
	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("list roles from workos: %w", err))
	}

	for _, wr := range wRoles {
		if wr.ID == p.ID {
			grants, err := s.grantsForRole(ctx, ac.ActiveOrganizationID, wr.Slug)
			if err != nil {
				return nil, gen.MakeUnexpected(err)
			}

			members, err := s.roles.ListMembers(ctx, workosOrgID)
			if err != nil {
				return nil, gen.MakeGatewayError(fmt.Errorf("list members: %w", err))
			}
			count := 0
			for _, m := range members {
				if m.Role.Slug == wr.Slug {
					count++
				}
			}

			return &gen.Role{
				ID:          wr.ID,
				Name:        wr.Name,
				Description: wr.Description,
				IsSystem:    wr.Type == "EnvironmentRole",
				Grants:      grants,
				MemberCount: count,
				CreatedAt:   wr.CreatedAt,
				UpdatedAt:   wr.UpdatedAt,
			}, nil
		}
	}

	return nil, gen.MakeNotFound(fmt.Errorf("role %q not found", p.ID))
}

func (s *Service) CreateRole(ctx context.Context, p *gen.CreateRolePayload) (*gen.Role, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	// Create the role in WorkOS.
	wr, err := s.roles.CreateRole(ctx, workosOrgID, workos.CreateRoleOpts{
		Name:        p.Name,
		Slug:        slugify(p.Name),
		Description: p.Description,
	})
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("create role in workos: %w", err))
	}

	// Sync scope grants to local DB.
	if len(p.Grants) > 0 {
		if err := s.syncGrants(ctx, ac.ActiveOrganizationID, wr.Slug, p.Grants); err != nil {
			return nil, gen.MakeUnexpected(fmt.Errorf("sync grants: %w", err))
		}
	}

	// Assign members if requested.
	if len(p.MemberIds) > 0 {
		members, err := s.roles.ListMembers(ctx, workosOrgID)
		if err != nil {
			return nil, gen.MakeGatewayError(fmt.Errorf("list members: %w", err))
		}
		// Build a lookup from user_id to membership_id.
		membershipByUser := make(map[string]string)
		for _, m := range members {
			membershipByUser[m.UserID] = m.ID
		}
		for _, uid := range p.MemberIds {
			if mid, ok := membershipByUser[uid]; ok {
				if _, err := s.roles.UpdateMemberRole(ctx, mid, wr.Slug); err != nil {
					s.logger.WarnContext(ctx, "failed to assign member to new role",
						slog.String("user_id", uid),
						slog.String("role_slug", wr.Slug),
						slog.Any("error", err),
					)
				}
			}
		}
	}

	grants, _ := s.grantsForRole(ctx, ac.ActiveOrganizationID, wr.Slug)

	return &gen.Role{
		ID:          wr.ID,
		Name:        wr.Name,
		Description: wr.Description,
		IsSystem:    false,
		Grants:      grants,
		MemberCount: len(p.MemberIds),
		CreatedAt:   wr.CreatedAt,
		UpdatedAt:   wr.UpdatedAt,
	}, nil
}

func (s *Service) UpdateRole(ctx context.Context, p *gen.UpdateRolePayload) (*gen.Role, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	// Find the existing role to get its slug.
	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("list roles: %w", err))
	}
	var currentSlug string
	for _, wr := range wRoles {
		if wr.ID == p.ID {
			currentSlug = wr.Slug
			break
		}
	}
	if currentSlug == "" {
		return nil, gen.MakeNotFound(fmt.Errorf("role %q not found", p.ID))
	}

	// Update role metadata in WorkOS (identified by slug, not ID).
	opts := workos.UpdateRoleOpts{
		Name:        p.Name,
		Description: p.Description,
	}
	wr, err := s.roles.UpdateRole(ctx, workosOrgID, currentSlug, opts)
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("update role in workos: %w", err))
	}

	// Sync grants if provided.
	if p.Grants != nil {
		if err := s.syncGrants(ctx, ac.ActiveOrganizationID, wr.Slug, p.Grants); err != nil {
			return nil, gen.MakeUnexpected(fmt.Errorf("sync grants: %w", err))
		}
	}

	grants, _ := s.grantsForRole(ctx, ac.ActiveOrganizationID, wr.Slug)

	members, _ := s.roles.ListMembers(ctx, workosOrgID)
	count := 0
	for _, m := range members {
		if m.Role.Slug == wr.Slug {
			count++
		}
	}

	return &gen.Role{
		ID:          wr.ID,
		Name:        wr.Name,
		Description: wr.Description,
		IsSystem:    wr.Type == "EnvironmentRole",
		Grants:      grants,
		MemberCount: count,
		CreatedAt:   wr.CreatedAt,
		UpdatedAt:   wr.UpdatedAt,
	}, nil
}

func (s *Service) DeleteRole(ctx context.Context, p *gen.DeleteRolePayload) error {
	ac, err := s.authContext(ctx)
	if err != nil {
		return err
	}

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return err
	}

	// Find the role to get its slug for grant cleanup.
	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return gen.MakeGatewayError(fmt.Errorf("list roles: %w", err))
	}
	var roleSlug string
	for _, wr := range wRoles {
		if wr.ID == p.ID {
			if wr.Type == "EnvironmentRole" {
				return gen.MakeForbidden(errors.New("cannot delete a system role"))
			}
			roleSlug = wr.Slug
			break
		}
	}
	if roleSlug == "" {
		return gen.MakeNotFound(fmt.Errorf("role %q not found", p.ID))
	}

	// Delete the role from WorkOS (identified by slug, not ID).
	if err := s.roles.DeleteRole(ctx, workosOrgID, roleSlug); err != nil {
		return gen.MakeGatewayError(fmt.Errorf("delete role from workos: %w", err))
	}

	// Clean up local grants for this role.
	q := repo.New(s.db)
	principalURN := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)
	if _, err := q.DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: ac.ActiveOrganizationID,
		PrincipalUrn:   principalURN,
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to clean up grants after role deletion",
			slog.String("role_slug", roleSlug),
			slog.Any("error", err),
		)
	}

	return nil
}

func (s *Service) ListScopes(_ context.Context, _ *gen.ListScopesPayload) (*gen.ListScopesResult, error) {
	return &gen.ListScopesResult{
		Scopes: []*gen.ScopeDefinition{
			{Slug: "org:read", Description: "View organization settings and metadata", ResourceType: "org"},
			{Slug: "org:admin", Description: "Manage organization settings, billing, and team", ResourceType: "org"},
			{Slug: "build:read", Description: "View projects, deployments, and build logs", ResourceType: "project"},
			{Slug: "build:write", Description: "Create and manage projects and deployments", ResourceType: "project"},
			{Slug: "mcp:read", Description: "View MCP server configurations", ResourceType: "mcp"},
			{Slug: "mcp:write", Description: "Create and manage MCP server configurations", ResourceType: "mcp"},
			{Slug: "mcp:connect", Description: "Connect to and use MCP servers", ResourceType: "mcp"},
		},
	}, nil
}

func (s *Service) ListMembers(ctx context.Context, _ *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	memberships, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("list members from workos: %w", err))
	}

	// Build slug→ID lookup so we return role IDs the frontend can match.
	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("list roles: %w", err))
	}
	slugToID := make(map[string]string, len(wRoles))
	for _, wr := range wRoles {
		slugToID[wr.Slug] = wr.ID
	}

	result := &gen.ListMembersResult{
		Members: make([]*gen.AccessMember, 0, len(memberships)),
	}

	for _, m := range memberships {
		user, err := s.roles.GetUser(ctx, m.UserID)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to fetch user for membership",
				slog.String("user_id", m.UserID),
				slog.Any("error", err),
			)
			continue
		}

		roleID := slugToID[m.Role.Slug]
		if roleID == "" {
			roleID = m.Role.Slug // fallback
		}

		member := &gen.AccessMember{
			ID:       m.UserID,
			Name:     user.FirstName + " " + user.LastName,
			Email:    user.Email,
			RoleID:   roleID,
			JoinedAt: m.CreatedAt,
		}
		if user.ProfilePictureURL != "" {
			member.PhotoURL = &user.ProfilePictureURL
		}

		result.Members = append(result.Members, member)
	}

	return result, nil
}

func (s *Service) UpdateMemberRole(ctx context.Context, p *gen.UpdateMemberRolePayload) (*gen.AccessMember, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, err
	}

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	// Find the membership for this user in this org.
	memberships, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("list members: %w", err))
	}

	var membershipID string
	for _, m := range memberships {
		if m.UserID == p.UserID {
			membershipID = m.ID
			break
		}
	}
	if membershipID == "" {
		return nil, gen.MakeNotFound(fmt.Errorf("membership not found for user %q", p.UserID))
	}

	// p.RoleID may be a WorkOS role ID (role_...) or a slug — resolve to slug.
	roleSlug := p.RoleID
	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("list roles: %w", err))
	}
	for _, wr := range wRoles {
		if wr.ID == p.RoleID {
			roleSlug = wr.Slug
			break
		}
	}

	updated, err := s.roles.UpdateMemberRole(ctx, membershipID, roleSlug)
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("update member role: %w", err))
	}

	user, err := s.roles.GetUser(ctx, updated.UserID)
	if err != nil {
		return nil, gen.MakeGatewayError(fmt.Errorf("get user: %w", err))
	}

	// Resolve slug back to role ID for the response.
	responseRoleID := updated.Role.Slug
	for _, wr := range wRoles {
		if wr.Slug == updated.Role.Slug {
			responseRoleID = wr.ID
			break
		}
	}

	member := &gen.AccessMember{
		ID:       updated.UserID,
		Name:     user.FirstName + " " + user.LastName,
		Email:    user.Email,
		RoleID:   responseRoleID,
		JoinedAt: updated.CreatedAt,
	}
	if user.ProfilePictureURL != "" {
		member.PhotoURL = &user.ProfilePictureURL
	}

	return member, nil
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// slugify creates a URL-safe slug from a role name.
// WorkOS requires organization role slugs to begin with "org-".
func slugify(name string) string {
	var b []byte
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b = append(b, byte(r))
		case r >= 'A' && r <= 'Z':
			b = append(b, byte(r-'A'+'a'))
		case r == ' ' || r == '-' || r == '_':
			if len(b) > 0 && b[len(b)-1] != '-' {
				b = append(b, '-')
			}
		}
	}
	// Trim trailing dash.
	if len(b) > 0 && b[len(b)-1] == '-' {
		b = b[:len(b)-1]
	}
	slug := string(b)
	if slug == "" {
		return ""
	}
	// WorkOS org roles must start with "org-".
	if len(slug) < 4 || slug[:4] != "org-" {
		slug = "org-" + slug
	}
	return slug
}
