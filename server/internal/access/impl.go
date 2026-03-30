package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	srv "github.com/speakeasy-api/gram/server/gen/http/access/server"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var (
	errWorkOSOrganizationNotLinked = errors.New("organization is not linked to WorkOS")
	errConnectedUserNotFound       = errors.New("connected user not found")
)

type roleProvider interface {
	ListRoles(ctx context.Context, orgID string) ([]workos.Role, error)
	CreateRole(ctx context.Context, orgID string, opts workos.CreateRoleOpts) (*workos.Role, error)
	UpdateRole(ctx context.Context, orgID string, roleSlug string, opts workos.UpdateRoleOpts) (*workos.Role, error)
	DeleteRole(ctx context.Context, orgID string, roleSlug string) error
	ListMembers(ctx context.Context, orgID string) ([]workos.Member, error)
	UpdateMemberRole(ctx context.Context, membershipID string, roleSlug string) (*workos.Member, error)
	GetUser(ctx context.Context, userID string) (*workos.User, error)
	ListOrgUsers(ctx context.Context, orgID string) (map[string]workos.User, error)
}

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	roles  roleProvider
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, roles roleProvider) *Service {
	logger = logger.With(attr.SlogComponent("access"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/access"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions),
		roles:  roles,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListRoles(ctx context.Context, _ *gen.ListRolesPayload) (*gen.ListRolesResult, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list roles from workos").Log(ctx, s.logger)
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
	}
	memberCounts := lo.CountValuesBy(members, func(member workos.Member) string {
		return member.RoleSlug
	})

	roles := make([]*gen.Role, 0, len(wRoles))
	for _, wr := range wRoles {
		role, err := buildRole(ctx, s.logger, s.db, ac.ActiveOrganizationID, wr, memberCounts[wr.Slug])
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}

	return &gen.ListRolesResult{Roles: roles}, nil
}

func (s *Service) GetRole(ctx context.Context, payload *gen.GetRolePayload) (*gen.Role, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list roles from workos").Log(ctx, s.logger)
	}

	role, ok := lo.Find(wRoles, func(role workos.Role) bool { return role.ID == payload.ID })
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, s.logger)
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
	}

	memberCounts := lo.CountValuesBy(members, func(member workos.Member) string {
		return member.RoleSlug
	})

	return buildRole(ctx, s.logger, s.db, ac.ActiveOrganizationID, role, memberCounts[role.Slug])
}

// CreateRole is creates a role for a user of a given organization.
// It is an idempotent operation intentionally ordered so that member assignment happens last.
// If WorkOS role creation succeeds but local grant sync fails, we return an
// error with no users assigned to the new role. That leaves a partially
// created role behind, but keeps the outcome safe and retryable: repeating the
// request can finish configuration without having granted accidental access.
func (s *Service) CreateRole(ctx context.Context, payload *gen.CreateRolePayload) (*gen.Role, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}

	roleSlug := slugify(payload.Name)
	wr, err := s.roles.CreateRole(ctx, workosOrgID, workos.CreateRoleOpts{
		Name:        payload.Name,
		Slug:        roleSlug,
		Description: payload.Description,
	})
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "create role in workos").Log(ctx, s.logger)
	}

	// Stop before assigning members if grant sync fails. That can leave behind a
	// newly created WorkOS role with no local grants, but it avoids assigning users
	// to a role whose effective permissions are incomplete or unknown. Returning an
	// error makes the setup retryable without creating accidental access.
	if err := syncGrants(ctx, s.logger, s.db, ac.ActiveOrganizationID, wr.Slug, roleGrantPayloads(payload.Grants)); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "sync grants for created role").Log(ctx, s.logger)
	}

	assignedCount := 0
	if len(payload.MemberIds) > 0 {
		members, err := s.roles.ListMembers(ctx, workosOrgID)
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
		}

		membershipByUser := lo.SliceToMap(members, func(member workos.Member) (string, string) {
			return member.UserID, member.ID
		})

		for _, userID := range payload.MemberIds {
			membershipID, ok := membershipByUser[userID]
			if !ok {
				continue
			}

			if _, err := s.roles.UpdateMemberRole(ctx, membershipID, wr.Slug); err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "assign members to created role").Log(ctx, s.logger)
			}

			assignedCount++
		}
	}

	return buildRole(ctx, s.logger, s.db, ac.ActiveOrganizationID, *wr, assignedCount)
}

func (s *Service) UpdateRole(ctx context.Context, payload *gen.UpdateRolePayload) (*gen.Role, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list roles from workos").Log(ctx, s.logger)
	}

	currentRole, ok := lo.Find(wRoles, func(role workos.Role) bool { return role.ID == payload.ID })
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, s.logger)
	}
	if isSystemRole(currentRole.Slug) {
		return nil, oops.E(oops.CodeBadRequest, nil, "system roles cannot be updated").Log(ctx, s.logger)
	}

	updatedRole, err := s.roles.UpdateRole(ctx, workosOrgID, currentRole.Slug, workos.UpdateRoleOpts{
		Name:        payload.Name,
		Description: payload.Description,
	})
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "update role in workos").Log(ctx, s.logger)
	}

	// As with role creation, member reassignment happens after local grant sync so
	// a failed sync never leaves users attached to a role with incomplete access.
	if payload.Grants != nil {
		if err := syncGrants(ctx, s.logger, s.db, ac.ActiveOrganizationID, currentRole.Slug, roleGrantPayloads(payload.Grants)); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "sync grants for updated role").Log(ctx, s.logger)
		}
	}

	if payload.MemberIds != nil {
		members, err := s.roles.ListMembers(ctx, workosOrgID)
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
		}

		membershipByUser := lo.SliceToMap(members, func(member workos.Member) (string, string) {
			return member.UserID, member.ID
		})

		for _, userID := range payload.MemberIds {
			membershipID, ok := membershipByUser[userID]
			if !ok {
				continue
			}

			if _, err := s.roles.UpdateMemberRole(ctx, membershipID, currentRole.Slug); err != nil {
				return nil, oops.E(oops.CodeGatewayError, err, "assign members to updated role").Log(ctx, s.logger)
			}
		}
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
	}

	memberCounts := lo.CountValuesBy(members, func(member workos.Member) string {
		return member.RoleSlug
	})

	return buildRole(ctx, s.logger, s.db, ac.ActiveOrganizationID, *updatedRole, memberCounts[currentRole.Slug])
}

func (s *Service) DeleteRole(ctx context.Context, payload *gen.DeleteRolePayload) error {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return err
	}

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return oops.E(oops.CodeGatewayError, err, "list roles from workos").Log(ctx, s.logger)
	}

	currentRole, ok := lo.Find(wRoles, func(role workos.Role) bool { return role.ID == payload.ID })
	if !ok {
		return oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, s.logger)
	}
	if isSystemRole(currentRole.Slug) {
		return oops.E(oops.CodeBadRequest, nil, "system roles cannot be deleted").Log(ctx, s.logger)
	}

	if _, err := repo.New(s.db).DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: ac.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, currentRole.Slug),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete grants for deleted role").Log(ctx, s.logger)
	}

	if err := s.roles.DeleteRole(ctx, workosOrgID, currentRole.Slug); err != nil {
		return oops.E(oops.CodeGatewayError, err, "delete role in workos").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) ListScopes(ctx context.Context, _ *gen.ListScopesPayload) (*gen.ListScopesResult, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	return &gen.ListScopesResult{Scopes: []*gen.ScopeDefinition{
		{Slug: string(ScopeOrgRead), Description: "Read organization metadata and members.", ResourceType: "org"},
		{Slug: string(ScopeOrgAdmin), Description: "Manage organization access and settings.", ResourceType: "org"},
		{Slug: string(ScopeBuildRead), Description: "View projects and build-related resources.", ResourceType: "project"},
		{Slug: string(ScopeBuildWrite), Description: "Create and modify projects and build-related resources.", ResourceType: "project"},
		{Slug: string(ScopeMCPRead), Description: "View MCP servers and configuration.", ResourceType: "mcp"},
		{Slug: string(ScopeMCPWrite), Description: "Create and modify MCP servers and configuration.", ResourceType: "mcp"},
		{Slug: string(ScopeMCPConnect), Description: "Connect to and use MCP servers.", ResourceType: "mcp"},
	}}, nil
}

func (s *Service) ListMembers(ctx context.Context, _ *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	_, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}

	roles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list roles from workos").Log(ctx, s.logger)
	}
	roleIDBySlug := make(map[string]string, len(roles))
	for _, role := range roles {
		roleIDBySlug[role.Slug] = role.ID
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
	}

	users, err := s.roles.ListOrgUsers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list org users from workos").Log(ctx, s.logger)
	}

	result := make([]*gen.AccessMember, 0, len(members))
	for _, member := range members {
		user, ok := users[member.UserID]
		if !ok {
			continue
		}

		result = append(result, &gen.AccessMember{
			ID:       user.ID,
			Name:     formatUserName(user),
			Email:    user.Email,
			PhotoURL: nil,
			RoleID:   roleIDBySlug[member.RoleSlug],
			JoinedAt: time.Time{}.UTC().Format(time.RFC3339),
		})
	}

	return &gen.ListMembersResult{Members: result}, nil
}

func (s *Service) UpdateMemberRole(ctx context.Context, payload *gen.UpdateMemberRolePayload) (*gen.AccessMember, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}

	roles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list roles from workos").Log(ctx, s.logger)
	}

	roleSlug := ""
	for _, role := range roles {
		if role.ID == payload.RoleID {
			roleSlug = role.Slug
			break
		}
	}
	if roleSlug == "" {
		return nil, oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, s.logger)
	}

	connectedUser, err := connectedUser(ctx, s.db, ac.ActiveOrganizationID, payload.UserID)
	switch {
	case errors.Is(err, errConnectedUserNotFound):
		return nil, oops.E(oops.CodeNotFound, nil, "member is not connected locally").Log(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load connected user").Log(ctx, s.logger)
	default:
	}
	if !connectedUser.WorkosID.Valid || connectedUser.WorkosID.String == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "member is not linked to WorkOS").Log(ctx, s.logger)
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list members from workos").Log(ctx, s.logger)
	}

	membershipID := ""
	for _, member := range members {
		if member.UserID == connectedUser.WorkosID.String {
			membershipID = member.ID
			break
		}
	}
	if membershipID == "" {
		return nil, oops.E(oops.CodeNotFound, nil, "member not found").Log(ctx, s.logger)
	}

	updatedMember, err := s.roles.UpdateMemberRole(ctx, membershipID, roleSlug)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "update member role in workos").Log(ctx, s.logger)
	}

	users, err := s.roles.ListOrgUsers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list org users from workos").Log(ctx, s.logger)
	}
	user, ok := users[updatedMember.UserID]
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "member user not found").Log(ctx, s.logger)
	}

	return &gen.AccessMember{
		ID:       connectedUser.ID,
		Name:     formatUserName(user),
		Email:    user.Email,
		PhotoURL: conv.FromPGText[string](connectedUser.PhotoUrl),
		RoleID:   payload.RoleID,
		JoinedAt: time.Time{}.UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) ListGrants(ctx context.Context, payload *gen.ListGrantsPayload) (*gen.ListGrantsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	rows, err := repo.New(s.db).ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   conv.PtrValOr(payload.PrincipalUrn, ""),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list principal grants").Log(ctx, s.logger)
	}

	grants := make([]*gen.Grant, len(rows))
	for i, row := range rows {
		grants[i] = grantFromRow(row)
	}

	return &gen.ListGrantsResult{Grants: grants}, nil
}

func (s *Service) UpsertGrants(ctx context.Context, payload *gen.UpsertGrantsPayload) (*gen.UpsertGrantsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to access grants").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tr := repo.New(dbtx)

	grants := make([]*gen.Grant, 0, len(payload.Grants))

	for _, form := range payload.Grants {
		if form == nil {
			continue
		}

		row, err := tr.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   form.PrincipalUrn,
			Scope:          form.Scope,
			Resource:       form.Resource,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to add or update grant").Log(ctx, s.logger)
		}

		grants = append(grants, grantFromRow(row))
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save updated grants").Log(ctx, s.logger)
	}

	return &gen.UpsertGrantsResult{Grants: grants}, nil
}

func (s *Service) RemoveGrants(ctx context.Context, payload *gen.RemoveGrantsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to access grants").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tr := repo.New(dbtx)

	for _, entry := range payload.Grants {
		if entry == nil {
			continue
		}

		_, err = tr.DeletePrincipalGrantByTuple(ctx, repo.DeletePrincipalGrantByTupleParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   entry.PrincipalUrn,
			Scope:          entry.Scope,
			Resource:       entry.Resource,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to remove grant").Log(ctx, s.logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to save grant removals").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) RemovePrincipalGrants(ctx context.Context, payload *gen.RemovePrincipalGrantsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	_, err := repo.New(s.db).DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   payload.PrincipalUrn,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to remove principal grants").Log(ctx, s.logger)
	}

	return nil
}

func isSystemRole(roleSlug string) bool {
	switch roleSlug {
	case "admin", "member":
		return true
	default:
		return false
	}
}

func roleGrantPayloads(grants []*gen.RoleGrant) []*RoleGrant {
	out := make([]*RoleGrant, 0, len(grants))
	for _, grant := range grants {
		if grant == nil {
			continue
		}

		out = append(out, &RoleGrant{
			Scope:     grant.Scope,
			Resources: append([]string(nil), grant.Resources...),
		})
	}

	return out
}

func formatUserName(user workos.User) string {
	switch {
	case user.FirstName != "" && user.LastName != "":
		return user.FirstName + " " + user.LastName
	case user.FirstName != "":
		return user.FirstName
	case user.LastName != "":
		return user.LastName
	default:
		return user.Email
	}
}

func buildRole(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, organizationID string, role workos.Role, memberCount int) (*gen.Role, error) {
	grants, err := grantsForRole(ctx, logger, db, organizationID, role.Slug)
	if err != nil {
		return nil, err
	}

	return &gen.Role{
		ID:          role.ID,
		Name:        role.Name,
		Description: role.Description,
		IsSystem:    isSystemRole(role.Slug),
		Grants:      grants,
		MemberCount: memberCount,
		CreatedAt:   conv.Default(role.CreatedAt, time.Time{}.UTC().Format(time.RFC3339)),
		UpdatedAt:   conv.Default(role.UpdatedAt, time.Time{}.UTC().Format(time.RFC3339)),
	}, nil
}

func connectedUser(ctx context.Context, db *pgxpool.Pool, organizationID string, userID string) (usersrepo.User, error) {
	hasRelationship, err := orgrepo.New(db).HasOrganizationUserRelationship(ctx, orgrepo.HasOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         userID,
	})
	if err != nil {
		return usersrepo.User{}, fmt.Errorf("check organization user relationship: %w", err)
	}
	if !hasRelationship {
		return usersrepo.User{}, errConnectedUserNotFound
	}

	user, err := usersrepo.New(db).GetUser(ctx, userID)
	if err != nil {
		return usersrepo.User{}, fmt.Errorf("get user %q: %w", userID, err)
	}

	return user, nil
}

func grantFromRow(row repo.PrincipalGrant) *gen.Grant {
	return &gen.Grant{
		ID:             row.ID.String(),
		OrganizationID: row.OrganizationID,
		PrincipalUrn:   row.PrincipalUrn.String(),
		PrincipalType:  row.PrincipalType,
		Scope:          row.Scope,
		Resource:       row.Resource,
		CreatedAt:      row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func slugify(name string) string {
	var b strings.Builder
	prevDash := false

	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '-' || r == '_':
			if b.Len() > 0 && !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}

	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return ""
	}
	if !strings.HasPrefix(slug, "org-") {
		slug = "org-" + slug
	}

	return slug
}

func (s *Service) workosOrgID(ctx context.Context, gramOrgID string) (string, error) {
	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, gramOrgID)
	if err != nil {
		return "", fmt.Errorf("get organization metadata: %w", err)
	}
	if !org.WorkosID.Valid || org.WorkosID.String == "" {
		return "", errWorkOSOrganizationNotLinked
	}

	return org.WorkosID.String, nil
}

func (s *Service) roleOrgContext(ctx context.Context) (*contextvalues.AuthContext, string, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil {
		return nil, "", oops.C(oops.CodeUnauthorized)
	}
	if s.roles == nil {
		return nil, "", oops.E(oops.CodeGatewayError, nil, "role provider is not configured").Log(ctx, s.logger)
	}

	workosOrgID, err := s.workosOrgID(ctx, ac.ActiveOrganizationID)
	switch {
	case errors.Is(err, errWorkOSOrganizationNotLinked):
		return nil, "", oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS").Log(ctx, s.logger)
	case err != nil:
		return nil, "", oops.E(oops.CodeUnexpected, err, "get organization metadata").Log(ctx, s.logger)
	default:
	}

	return ac, workosOrgID, nil
}
