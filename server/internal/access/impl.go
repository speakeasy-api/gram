package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	srv "github.com/speakeasy-api/gram/server/gen/http/access/server"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var (
	errConnectedUserNotFound = errors.New("connected user not found")
	// Custom role names become stable slugs and user-facing identifiers, so keep
	// them to a predictable ASCII set instead of silently normalizing symbols.
	validRoleNamePattern = regexp.MustCompile(`^[A-Za-z0-9 _-]+$`)
)

type RoleProvider interface {
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
	access *Manager
	roles  RoleProvider
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, roles RoleProvider, accessManager *Manager) *Service {
	logger = logger.With(attr.SlogComponent("access"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/access"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, accessManager),
		access: accessManager,
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

// ListRoles treats WorkOS as the source of truth for role records while Gram
// remains the source of truth for role grants.
func (s *Service) ListRoles(ctx context.Context, _ *gen.ListRolesPayload) (*gen.ListRolesResult, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.access.Require(ctx, Check{Scope: ScopeOrgRead, ResourceID: ac.ActiveOrganizationID}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles from workos").Log(ctx, s.logger)
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members from workos").Log(ctx, s.logger)
	}
	memberCounts := countMembersByRoleSlug(members)

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

// GetRole returns the WorkOS role definition enriched with Gram's local grant
// state so callers see the complete effective role configuration in one place.
func (s *Service) GetRole(ctx context.Context, payload *gen.GetRolePayload) (*gen.Role, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.access.Require(ctx, Check{Scope: ScopeOrgRead, ResourceID: ac.ActiveOrganizationID}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessRoleID(payload.ID),
	)

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles from workos").Log(ctx, s.logger)
	}

	role, ok := findRoleByID(wRoles, payload.ID)
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, s.logger)
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members from workos").Log(ctx, s.logger)
	}

	memberCounts := countMembersByRoleSlug(members)

	return buildRole(ctx, s.logger, s.db, ac.ActiveOrganizationID, role, memberCounts[role.Slug])
}

// CreateRole creates a role for a user of a given organization.
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
	if err := s.access.Require(ctx, Check{Scope: ScopeOrgAdmin, ResourceID: ac.ActiveOrganizationID}); err != nil {
		return nil, err
	}

	roleSlug, slugErr := slugify(payload.Name)
	if slugErr != nil {
		return nil, slugErr
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
		attr.SlogAccessRoleSlug(roleSlug),
	)
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessRoleSlug(roleSlug),
	)

	wr, err := s.roles.CreateRole(ctx, workosOrgID, workos.CreateRoleOpts{
		Name:        payload.Name,
		Slug:        roleSlug,
		Description: payload.Description,
	})
	var apiErr *workos.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.StatusCode == 409:
		wRoles, listErr := s.roles.ListRoles(ctx, workosOrgID)
		if listErr != nil {
			return nil, oops.E(oops.CodeUnexpected, listErr, "list roles after create conflict").Log(ctx, logger)
		}

		var existingRole workos.Role
		ok := false
		for _, candidate := range wRoles {
			if candidate.Slug == roleSlug {
				existingRole = candidate
				ok = true
				break
			}
		}
		if !ok {
			return nil, oops.E(oops.CodeUnexpected, err, "create role in workos").Log(ctx, logger)
		}

		wr = &existingRole
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "create role in workos").Log(ctx, logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessRoleID(wr.ID),
	)

	// Stop before assigning members if grant sync fails. That can leave behind a
	// newly created WorkOS role with no local grants, but it avoids assigning users
	// to a role whose effective permissions are incomplete or unknown. Returning an
	// error makes the setup retryable without creating accidental access.
	if err := syncGrants(ctx, s.logger, s.db, ac.ActiveOrganizationID, wr.Slug, roleGrantPayloads(payload.Grants)); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "sync grants for created role").Log(ctx, logger)
	}

	assignedCount := 0
	if len(payload.MemberIds) > 0 {
		members, err := s.roles.ListMembers(ctx, workosOrgID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list members from workos").Log(ctx, logger)
		}

		membershipByUser := membershipsByUserID(members)

		for _, userID := range payload.MemberIds {
			membershipID, ok := membershipByUser[userID]
			if !ok {
				continue
			}

			if _, err := s.roles.UpdateMemberRole(ctx, membershipID, wr.Slug); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "assign members to created role").Log(ctx, logger)
			}

			assignedCount++
		}
	}

	createdRole, err := buildRole(ctx, logger, s.db, ac.ActiveOrganizationID, *wr, assignedCount)
	if err != nil {
		return nil, err
	}

	if err := audit.LogAccessRoleCreate(ctx, s.db, audit.LogAccessRoleCreateEvent{
		OrganizationID:    ac.ActiveOrganizationID,
		Actor:             urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:  ac.Email,
		ActorSlug:         nil,
		RoleID:            wr.ID,
		RoleName:          createdRole.Name,
		RoleSlug:          wr.Slug,
		RoleSnapshotAfter: createdRole,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log access role creation").Log(ctx, logger)
	}

	return createdRole, nil
}

// UpdateRole preserves the same split of responsibilities as creation: WorkOS
// owns role identity and membership, while Gram owns the role's grant set.
func (s *Service) UpdateRole(ctx context.Context, payload *gen.UpdateRolePayload) (*gen.Role, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.access.Require(ctx, Check{Scope: ScopeOrgAdmin, ResourceID: ac.ActiveOrganizationID}); err != nil {
		return nil, err
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
		attr.SlogAccessRoleID(payload.ID),
	)
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessRoleID(payload.ID),
	)

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles from workos").Log(ctx, logger)
	}

	currentRole, ok := findRoleByID(wRoles, payload.ID)
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, logger)
	}
	logger = logger.With(attr.SlogAccessRoleSlug(currentRole.Slug))
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessRoleSlug(currentRole.Slug),
	)
	if isSystemRole(currentRole.Slug) {
		return nil, oops.E(oops.CodeBadRequest, nil, "system roles cannot be updated").Log(ctx, logger)
	}
	if payload.Name != nil {
		if _, err := slugify(*payload.Name); err != nil {
			return nil, err
		}
	}

	membersBefore, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members from workos").Log(ctx, logger)
	}
	memberCountsBefore := countMembersByRoleSlug(membersBefore)
	existingRole, err := buildRole(ctx, logger, s.db, ac.ActiveOrganizationID, currentRole, memberCountsBefore[currentRole.Slug])
	if err != nil {
		return nil, err
	}

	updatedRole, err := s.roles.UpdateRole(ctx, workosOrgID, currentRole.Slug, workos.UpdateRoleOpts{
		Name:        payload.Name,
		Description: payload.Description,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update role in workos").Log(ctx, logger)
	}

	// As with role creation, member reassignment happens after local grant sync so
	// a failed sync never leaves users attached to a role with incomplete access.
	if payload.Grants != nil {
		if err := syncGrants(ctx, s.logger, s.db, ac.ActiveOrganizationID, currentRole.Slug, roleGrantPayloads(payload.Grants)); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "sync grants for updated role").Log(ctx, logger)
		}
	}

	if payload.MemberIds != nil {
		membershipByUser := membershipsByUserID(membersBefore)

		for _, userID := range payload.MemberIds {
			membershipID, ok := membershipByUser[userID]
			if !ok {
				continue
			}

			if _, err := s.roles.UpdateMemberRole(ctx, membershipID, currentRole.Slug); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "assign members to updated role").Log(ctx, logger)
			}
		}
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members from workos").Log(ctx, logger)
	}

	memberCounts := countMembersByRoleSlug(members)
	updatedRoleView, err := buildRole(ctx, logger, s.db, ac.ActiveOrganizationID, *updatedRole, memberCounts[updatedRole.Slug])
	if err != nil {
		return nil, err
	}

	if err := audit.LogAccessRoleUpdate(ctx, s.db, audit.LogAccessRoleUpdateEvent{
		OrganizationID:     ac.ActiveOrganizationID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:   ac.Email,
		ActorSlug:          nil,
		RoleID:             updatedRole.ID,
		RoleName:           updatedRoleView.Name,
		RoleSlug:           updatedRole.Slug,
		RoleSnapshotBefore: existingRole,
		RoleSnapshotAfter:  updatedRoleView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log access role update").Log(ctx, logger)
	}

	return updatedRoleView, nil
}

// DeleteRole removes local grants before deleting the WorkOS role so retries can
// still complete cleanup if the external delete fails.
func (s *Service) DeleteRole(ctx context.Context, payload *gen.DeleteRolePayload) error {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return err
	}
	if err := s.access.Require(ctx, Check{Scope: ScopeOrgAdmin, ResourceID: ac.ActiveOrganizationID}); err != nil {
		return err
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
		attr.SlogAccessRoleID(payload.ID),
	)
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessRoleID(payload.ID),
	)

	wRoles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list roles from workos").Log(ctx, logger)
	}

	currentRole, ok := findRoleByID(wRoles, payload.ID)
	if !ok {
		return oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, logger)
	}
	logger = logger.With(attr.SlogAccessRoleSlug(currentRole.Slug))
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessRoleSlug(currentRole.Slug),
	)
	if isSystemRole(currentRole.Slug) {
		return oops.E(oops.CodeBadRequest, nil, "system roles cannot be deleted").Log(ctx, logger)
	}

	if _, err := repo.New(s.db).DeletePrincipalGrantsByPrincipal(ctx, repo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: ac.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, currentRole.Slug),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete grants for deleted role").Log(ctx, logger)
	}

	if err := s.roles.DeleteRole(ctx, workosOrgID, currentRole.Slug); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete role in workos").Log(ctx, logger)
	}

	if err := audit.LogAccessRoleDelete(ctx, s.db, audit.LogAccessRoleDeleteEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		RoleID:           currentRole.ID,
		RoleName:         currentRole.Name,
		RoleSlug:         currentRole.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log access role deletion").Log(ctx, logger)
	}

	return nil
}

// ListScopes exposes the stable set of grantable scopes so clients can build
// role editing UX without hardcoding permission definitions.
func (s *Service) ListScopes(ctx context.Context, _ *gen.ListScopesPayload) (*gen.ListScopesResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}
	if err := s.access.Require(ctx, Check{Scope: ScopeOrgRead, ResourceID: ac.ActiveOrganizationID}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

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

// ListMembers follows the original access API contract by returning WorkOS user
// identifiers while decorating them with the role information the UI needs.
func (s *Service) ListMembers(ctx context.Context, _ *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	_, workosOrgID, err := s.roleOrgContext(ctx)
	ac, _ := s.authContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.access.Require(ctx, Check{Scope: ScopeOrgRead, ResourceID: ac.ActiveOrganizationID}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	roles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles from workos").Log(ctx, s.logger)
	}
	roleIDBySlug := make(map[string]string, len(roles))
	for _, role := range roles {
		roleIDBySlug[role.Slug] = role.ID
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members from workos").Log(ctx, s.logger)
	}

	users, err := s.roles.ListOrgUsers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list org users from workos").Log(ctx, s.logger)
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
			JoinedAt: conv.Default(member.CreatedAt, time.Time{}.UTC().Format(time.RFC3339)),
		})
	}

	return &gen.ListMembersResult{Members: result}, nil
}

// ListGrants returns the effective grants for the current user by combining
// direct user grants with grants inherited from their currently assigned role.
func (s *Service) ListGrants(ctx context.Context, _ *gen.ListGrantsPayload) (*gen.ListUserGrantsResult, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
		attr.SlogAccessMemberID(ac.UserID),
	)
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessMemberID(ac.UserID),
	)

	connectedUser, err := connectedUser(ctx, s.db, ac.ActiveOrganizationID, ac.UserID)
	switch {
	case errors.Is(err, errConnectedUserNotFound):
		return nil, oops.E(oops.CodeNotFound, nil, "current user is not connected locally").Log(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load connected user").Log(ctx, logger)
	}

	principals := []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID)}
	roleSlugs, err := s.memberRoleSlugs(ctx, workosOrgID, connectedUser.WorkosID.String)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members from workos").Log(ctx, logger)
	}
	for _, roleSlug := range roleSlugs {
		principals = append(principals, urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug))
	}
	if len(roleSlugs) == 1 {
		logger = logger.With(attr.SlogAccessRoleSlug(roleSlugs[0]))
		trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSlug(roleSlugs[0]))
	}

	grants, err := LoadGrants(ctx, s.db, ac.ActiveOrganizationID, principals)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load effective user grants").Log(ctx, logger)
	}

	return &gen.ListUserGrantsResult{Grants: grantsFromRows(grants.rows)}, nil
}

// UpdateMemberRole is intentionally stricter than member listing: it only
// mutates access for users Gram knows are connected to the local organization.
func (s *Service) UpdateMemberRole(ctx context.Context, payload *gen.UpdateMemberRolePayload) (*gen.AccessMember, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.access.Require(ctx, Check{Scope: ScopeOrgAdmin, ResourceID: ac.ActiveOrganizationID}); err != nil {
		return nil, err
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
		attr.SlogAccessMemberID(payload.UserID),
		attr.SlogAccessRoleID(payload.RoleID),
	)
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessMemberID(payload.UserID),
		attr.AccessRoleID(payload.RoleID),
	)

	roles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles from workos").Log(ctx, logger)
	}

	roleSlug := ""
	for _, role := range roles {
		if role.ID == payload.RoleID {
			roleSlug = role.Slug
			break
		}
	}
	if roleSlug == "" {
		return nil, oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, logger)
	}
	logger = logger.With(attr.SlogAccessRoleSlug(roleSlug))
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessRoleSlug(roleSlug),
	)

	connectedUser, err := connectedUser(ctx, s.db, ac.ActiveOrganizationID, payload.UserID)
	switch {
	case errors.Is(err, errConnectedUserNotFound):
		return nil, oops.E(oops.CodeNotFound, nil, "member is not connected locally").Log(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load connected user").Log(ctx, logger)
	}
	if !connectedUser.WorkosID.Valid || connectedUser.WorkosID.String == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "member is not linked to WorkOS").Log(ctx, logger)
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members from workos").Log(ctx, logger)
	}

	roleIDBySlug := make(map[string]string, len(roles))
	for _, role := range roles {
		roleIDBySlug[role.Slug] = role.ID
	}

	membershipID := ""
	var existingMember workos.Member
	for _, member := range members {
		if member.UserID == connectedUser.WorkosID.String {
			membershipID = member.ID
			existingMember = member
			break
		}
	}
	if membershipID == "" {
		return nil, oops.E(oops.CodeNotFound, nil, "member not found").Log(ctx, logger)
	}

	updatedMember, err := s.roles.UpdateMemberRole(ctx, membershipID, roleSlug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update member role in workos").Log(ctx, logger)
	}

	users, err := s.roles.ListOrgUsers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list org users from workos").Log(ctx, logger)
	}
	user, ok := users[updatedMember.UserID]
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "member user not found").Log(ctx, logger)
	}

	beforeMember := &gen.AccessMember{
		ID:       connectedUser.ID,
		Name:     formatUserName(user),
		Email:    user.Email,
		PhotoURL: conv.FromPGText[string](connectedUser.PhotoUrl),
		RoleID:   roleIDBySlug[existingMember.RoleSlug],
		JoinedAt: conv.Default(existingMember.CreatedAt, time.Time{}.UTC().Format(time.RFC3339)),
	}
	afterMember := &gen.AccessMember{
		ID:       connectedUser.ID,
		Name:     formatUserName(user),
		Email:    user.Email,
		PhotoURL: conv.FromPGText[string](connectedUser.PhotoUrl),
		RoleID:   payload.RoleID,
		JoinedAt: conv.Default(updatedMember.CreatedAt, time.Time{}.UTC().Format(time.RFC3339)),
	}

	if err := audit.LogAccessMemberRoleUpdate(ctx, s.db, audit.LogAccessMemberRoleUpdateEvent{
		OrganizationID:       ac.ActiveOrganizationID,
		Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:     ac.Email,
		ActorSlug:            nil,
		MemberID:             connectedUser.ID,
		MemberName:           afterMember.Name,
		MemberEmail:          afterMember.Email,
		MemberSnapshotBefore: beforeMember,
		MemberSnapshotAfter:  afterMember,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log access member role update").Log(ctx, logger)
	}

	return afterMember, nil
}

func (s *Service) authContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil {
		return nil, errors.New("missing auth context")
	}

	return ac, nil
}

func (s *Service) roleOrgContext(ctx context.Context) (*contextvalues.AuthContext, string, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, "", oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, "", oops.E(oops.CodeUnexpected, err, "get organization metadata").Log(ctx, s.logger)
	}
	if !org.WorkosID.Valid || org.WorkosID.String == "" {
		return nil, "", oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS").Log(ctx, s.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	return ac, org.WorkosID.String, nil
}

func isSystemRole(roleSlug string) bool {
	return roleSlug == "admin" || roleSlug == "member"
}

func roleGrantPayloads(grants []*gen.RoleGrant) []*RoleGrant {
	out := make([]*RoleGrant, 0, len(grants))
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		var resources []string
		if grant.Resources != nil {
			resources = append([]string{}, grant.Resources...)
		}

		out = append(out, &RoleGrant{
			Scope:     grant.Scope,
			Resources: resources,
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

func countMembersByRoleSlug(members []workos.Member) map[string]int {
	counts := make(map[string]int, len(members))
	for _, member := range members {
		counts[member.RoleSlug]++
	}

	return counts
}

func membershipsByUserID(members []workos.Member) map[string]string {
	membershipByUser := make(map[string]string, len(members))
	for _, member := range members {
		membershipByUser[member.UserID] = member.ID
	}

	return membershipByUser
}

func (s *Service) memberRoleSlugs(ctx context.Context, workosOrgID string, workosUserID string) ([]string, error) {
	if workosUserID == "" {
		return nil, nil
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, fmt.Errorf("list members for role lookup: %w", err)
	}

	roleSlugs := make([]string, 0, len(members))
	seenRoleSlugs := make(map[string]struct{}, len(members))

	for _, member := range members {
		if member.UserID != workosUserID || member.RoleSlug == "" {
			continue
		}
		if _, ok := seenRoleSlugs[member.RoleSlug]; ok {
			continue
		}

		seenRoleSlugs[member.RoleSlug] = struct{}{}
		roleSlugs = append(roleSlugs, member.RoleSlug)
	}

	return roleSlugs, nil
}

func findRoleByID(roles []workos.Role, id string) (workos.Role, bool) {
	for _, role := range roles {
		if role.ID == id {
			return role, true
		}
	}

	var zero workos.Role
	return zero, false
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

func slugify(name string) (string, error) {
	slug := conv.ToSlug(strings.ReplaceAll(name, "_", " "))
	if slug == "" {
		return "", oops.E(oops.CodeBadRequest, nil, "role name must contain at least one letter or digit")
	}
	if !validRoleNamePattern.MatchString(name) {
		return "", oops.E(oops.CodeBadRequest, nil, "role name contains invalid characters")
	}
	if !strings.HasPrefix(slug, "org-") {
		slug = "org-" + slug
	}

	return slug, nil
}
