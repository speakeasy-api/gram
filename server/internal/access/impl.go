package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	"github.com/speakeasy-api/gram/server/internal/authz"
	chrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	pfRepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
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
	GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*workos.Member, error)
}

// FeatureCacheWriter updates the Redis cache entry for a feature flag after a
// direct DB write, keeping the cache consistent with the authoritative state.
type FeatureCacheWriter interface {
	UpdateFeatureCache(ctx context.Context, organizationID string, feature productfeatures.Feature, enabled bool)
}

type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	chConn       driver.Conn
	auth         *auth.Auth
	authz        *authz.Engine
	roles        RoleProvider
	featureCache FeatureCacheWriter
	audit        *audit.Logger
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	chConn driver.Conn,
	sessions *sessions.Manager,
	roles RoleProvider,
	authz *authz.Engine,
	featureCache FeatureCacheWriter,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("access"))

	return &Service{
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/access"),
		logger:       logger,
		db:           db,
		chConn:       chConn,
		auth:         auth.New(logger, db, sessions, authz),
		authz:        authz,
		roles:        roles,
		featureCache: featureCache,
		audit:        auditLogger,
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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
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
	memberCounts, err := s.localMemberCounts(ctx, ac.ActiveOrganizationID, members)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count local members by role").Log(ctx, s.logger)
	}

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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
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

	memberCounts, err := s.localMemberCounts(ctx, ac.ActiveOrganizationID, members)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count local members by role").Log(ctx, s.logger)
	}

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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
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
	if err := authz.SyncGrants(ctx, s.logger, s.db, ac.ActiveOrganizationID, wr.Slug, roleGrantPayloads(payload.Grants)); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "sync grants for created role").Log(ctx, logger)
	}

	assignedWorkosIDs := make([]string, 0, len(payload.MemberIds))
	if len(payload.MemberIds) > 0 {
		// payload.MemberIds are Gram user IDs (returned by ListMembers).
		// Resolve them to WorkOS user IDs so we can look up WorkOS memberships.
		gramToWorkos, err := gramToWorkosIDMap(ctx, s.db, payload.MemberIds)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "resolve gram user ids to workos ids").Log(ctx, logger)
		}

		members, err := s.roles.ListMembers(ctx, workosOrgID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list members from workos").Log(ctx, logger)
		}

		membershipByUser := membershipsByUserID(members)

		for _, gramID := range payload.MemberIds {
			workosID, ok := gramToWorkos[gramID]
			if !ok {
				continue
			}
			membershipID, ok := membershipByUser[workosID]
			if !ok {
				continue
			}

			if _, err := s.roles.UpdateMemberRole(ctx, membershipID, wr.Slug); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "assign members to created role").Log(ctx, logger)
			}

			assignedWorkosIDs = append(assignedWorkosIDs, workosID)
		}
		s.authz.InvalidateAllRoleCaches(ctx, ac.ActiveOrganizationID)
	}

	// Only count assigned members who have local Gram accounts and are
	// connected to this org, consistent with how ListMembers filters users.
	assignedCount := 0
	if len(assignedWorkosIDs) > 0 {
		localRows, err := usersrepo.New(s.db).GetConnectedUsersByWorkosIDs(ctx, usersrepo.GetConnectedUsersByWorkosIDsParams{
			WorkosIds:      assignedWorkosIDs,
			OrganizationID: ac.ActiveOrganizationID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get connected users by workos ids: %w", err), "resolve local assigned members").Log(ctx, logger)
		}
		assignedCount = len(localRows)
	}

	createdRole, err := buildRole(ctx, logger, s.db, ac.ActiveOrganizationID, *wr, assignedCount)
	if err != nil {
		return nil, err
	}

	if err := s.audit.LogAccessRoleCreate(ctx, s.db, audit.LogAccessRoleCreateEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		RoleID:           wr.ID,
		RoleName:         createdRole.Name,
		RoleSlug:         wr.Slug,
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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
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
	sysRole := isSystemRole(currentRole.Slug)
	if sysRole && (payload.Name != nil || payload.Description != nil || payload.Grants != nil) {
		return nil, oops.E(oops.CodeBadRequest, nil, "system role properties cannot be updated, only member assignment is allowed").Log(ctx, logger)
	}
	if sysRole && payload.MemberIds == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "system role update requires member_ids").Log(ctx, logger)
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
	memberCountsBefore, err := s.localMemberCounts(ctx, ac.ActiveOrganizationID, membersBefore)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count local members by role").Log(ctx, logger)
	}
	existingRole, err := buildRole(ctx, logger, s.db, ac.ActiveOrganizationID, currentRole, memberCountsBefore[currentRole.Slug])
	if err != nil {
		return nil, err
	}

	// System roles are immutable in WorkOS — skip the role update call and grant sync.
	var updatedRole *workos.Role
	if sysRole {
		updatedRole = &currentRole
	} else {
		updatedRole, err = s.roles.UpdateRole(ctx, workosOrgID, currentRole.Slug, workos.UpdateRoleOpts{
			Name:        payload.Name,
			Description: payload.Description,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "update role in workos").Log(ctx, logger)
		}

		// As with role creation, member reassignment happens after local grant sync so
		// a failed sync never leaves users attached to a role with incomplete access.
		if payload.Grants != nil {
			if err := authz.SyncGrants(ctx, s.logger, s.db, ac.ActiveOrganizationID, currentRole.Slug, roleGrantPayloads(payload.Grants)); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "sync grants for updated role").Log(ctx, logger)
			}
		}
	}

	if payload.MemberIds != nil {
		gramToWorkos, err := gramToWorkosIDMap(ctx, s.db, payload.MemberIds)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "resolve gram user ids to workos ids").Log(ctx, logger)
		}

		membershipByUser := membershipsByUserID(membersBefore)

		for _, gramID := range payload.MemberIds {
			workosID, ok := gramToWorkos[gramID]
			if !ok {
				continue
			}
			membershipID, ok := membershipByUser[workosID]
			if !ok {
				continue
			}

			if _, err := s.roles.UpdateMemberRole(ctx, membershipID, currentRole.Slug); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "assign members to updated role").Log(ctx, logger)
			}
		}
		s.authz.InvalidateAllRoleCaches(ctx, ac.ActiveOrganizationID)
	}

	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members from workos").Log(ctx, logger)
	}

	memberCounts, err := s.localMemberCounts(ctx, ac.ActiveOrganizationID, members)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count local members by role").Log(ctx, logger)
	}
	updatedRoleView, err := buildRole(ctx, logger, s.db, ac.ActiveOrganizationID, *updatedRole, memberCounts[updatedRole.Slug])
	if err != nil {
		return nil, err
	}

	if err := s.audit.LogAccessRoleUpdate(ctx, s.db, audit.LogAccessRoleUpdateEvent{
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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
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

	// WorkOS rejects deleting a role that still has members assigned, so move
	// any assigned members to the default member role first.
	members, err := s.roles.ListMembers(ctx, workosOrgID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list members from workos").Log(ctx, logger)
	}
	reassigned := false
	for _, m := range members {
		if m.RoleSlug != currentRole.Slug {
			continue
		}
		if _, err := s.roles.UpdateMemberRole(ctx, m.ID, authz.SystemRoleMember); err != nil {
			if reassigned {
				s.authz.InvalidateAllRoleCaches(ctx, ac.ActiveOrganizationID)
			}
			return oops.E(oops.CodeUnexpected, err, "reassign member to default role").Log(ctx, logger)
		}
		reassigned = true
	}
	if reassigned {
		s.authz.InvalidateAllRoleCaches(ctx, ac.ActiveOrganizationID)
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

	if err := s.audit.LogAccessRoleDelete(ctx, s.db, audit.LogAccessRoleDeleteEvent{
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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	return &gen.ListScopesResult{Scopes: []*gen.ScopeDefinition{
		{Slug: string(authz.ScopeOrgRead), Description: "Read organization metadata and members.", ResourceType: "org"},
		{Slug: string(authz.ScopeOrgAdmin), Description: "Manage organization access and settings.", ResourceType: "org"},
		{Slug: string(authz.ScopeProjectRead), Description: "View projects and project-related resources.", ResourceType: "project"},
		{Slug: string(authz.ScopeProjectWrite), Description: "Create and modify projects and project-related resources.", ResourceType: "project"},
		{Slug: string(authz.ScopeMCPRead), Description: "View MCP servers and configuration.", ResourceType: "mcp"},
		{Slug: string(authz.ScopeMCPWrite), Description: "Create and modify MCP servers and configuration.", ResourceType: "mcp"},
		{Slug: string(authz.ScopeMCPConnect), Description: "Connect to and use MCP servers.", ResourceType: "mcp"},
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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
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

	// Batch-resolve WorkOS user IDs to local Gram users, filtering to only
	// those connected to this organization via organization_user_relationships.
	// This single joined query prevents the list from surfacing users who
	// exist in Gram but aren't connected to the current org (which would
	// cause UpdateMemberRole to fail).
	workosIDs := make([]string, 0, len(users))
	for workosUID := range users {
		workosIDs = append(workosIDs, workosUID)
	}
	localUserRows, err := usersrepo.New(s.db).GetConnectedUsersByWorkosIDs(ctx, usersrepo.GetConnectedUsersByWorkosIDsParams{
		WorkosIds:      workosIDs,
		OrganizationID: ac.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve connected users by workos ids").Log(ctx, s.logger)
	}
	localUsers := make(map[string]usersrepo.User, len(localUserRows))
	for _, u := range localUserRows {
		if u.WorkosID.Valid {
			localUsers[u.WorkosID.String] = u
		}
	}

	result := make([]*gen.AccessMember, 0, len(members))
	for _, member := range members {
		user, ok := users[member.UserID]
		if !ok {
			continue
		}

		local, ok := localUsers[member.UserID]
		if !ok {
			continue
		}

		result = append(result, &gen.AccessMember{
			ID:       local.ID,
			Name:     formatUserName(user),
			Email:    user.Email,
			PhotoURL: conv.FromPGText[string](local.PhotoUrl),
			RoleID:   roleIDBySlug[member.RoleSlug],
			JoinedAt: conv.Default(member.CreatedAt, time.Time{}.UTC().Format(time.RFC3339)),
		})
	}

	return &gen.ListMembersResult{Members: result}, nil
}

// ListGrants returns the effective grants for the current user by combining
// direct user grants with grants inherited from their currently assigned role.
func (s *Service) ListGrants(ctx context.Context, _ *gen.ListGrantsPayload) (*gen.ListUserGrantsResult, error) {
	// Return override scopes when active so the frontend sees the same restricted
	// set as the enforcement layer.
	if overrides, ok := s.authz.GetScopeOverrides(ctx); ok {
		return &gen.ListUserGrantsResult{Grants: listRoleGrantsFromGrants(authz.GrantsFromOverrides(overrides))}, nil
	}

	enforce, err := s.authz.ShouldEnforce(ctx)
	if err != nil {
		return nil, err
	}
	if !enforce {
		return &gen.ListUserGrantsResult{Grants: allScopesGrants()}, nil
	}

	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}

	// Admins impersonating a customer org won't have an organization_users row
	// (the Info endpoint intentionally skips that upsert). Return full scopes so
	// the MembershipSyncGuard doesn't block the dashboard.
	if ac.IsAdmin {
		if _, hasOverride := contextvalues.GetAdminOverrideFromContext(ctx); hasOverride {
			return &gen.ListUserGrantsResult{Grants: allScopesGrants()}, nil
		}
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
		return nil, oops.E(oops.CodeNotFound, nil, "current user has not joined this organization").Log(ctx, logger)
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

	grants, err := authz.LoadGrants(ctx, s.db, ac.ActiveOrganizationID, principals)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load effective user grants").Log(ctx, logger)
	}

	return &gen.ListUserGrantsResult{Grants: listRoleGrantsFromGrants(grants)}, nil
}

// UpdateMemberRole is intentionally stricter than member listing: it only
// mutates access for users Gram knows are connected to the local organization.
func (s *Service) UpdateMemberRole(ctx context.Context, payload *gen.UpdateMemberRolePayload) (*gen.AccessMember, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
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
		return nil, oops.E(oops.CodeNotFound, nil, "member has not joined this organization").Log(ctx, logger)
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
	s.authz.InvalidateRoleCache(ctx, payload.UserID, ac.ActiveOrganizationID)

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

	if err := s.audit.LogAccessMemberRoleUpdate(ctx, s.db, audit.LogAccessMemberRoleUpdateEvent{
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
	return roleSlug == authz.SystemRoleAdmin || roleSlug == authz.SystemRoleMember
}

func roleGrantPayloads(grants []*gen.RoleGrant) []*authz.RoleGrant {
	out := make([]*authz.RoleGrant, 0, len(grants))
	for _, grant := range grants {
		if grant == nil {
			continue
		}

		var selectors []authz.Selector
		if grant.Selectors != nil {
			selectors = make([]authz.Selector, 0, len(grant.Selectors))
		}
		for _, s := range grant.Selectors {
			if s == nil {
				continue
			}
			selectors = append(selectors, genSelectorToAuthz(s))
		}

		out = append(out, &authz.RoleGrant{
			Scope:     grant.Scope,
			Selectors: selectors,
		})
	}

	return out
}

func genSelectorToAuthz(s *gen.Selector) authz.Selector {
	sel := authz.Selector{
		"resource_kind": s.ResourceKind,
		"resource_id":   s.ResourceID,
	}
	if s.Disposition != nil {
		sel["disposition"] = *s.Disposition
	}
	if s.Tool != nil {
		sel["tool"] = *s.Tool
	}
	return sel
}

func authzSelectorToGen(sel authz.Selector) *gen.Selector {
	s := &gen.Selector{
		ResourceKind: sel["resource_kind"],
		ResourceID:   sel["resource_id"],
		Disposition:  nil,
		Tool:         nil,
	}
	if v, ok := sel["disposition"]; ok {
		s.Disposition = &v
	}
	if v, ok := sel["tool"]; ok {
		s.Tool = &v
	}
	return s
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

// localMemberCounts counts WorkOS members per role slug, but only for members
// who have a local Gram account and are connected to the given organization
// via organization_user_relationships. This ensures counts match what
// ListMembers returns.
func (s *Service) localMemberCounts(ctx context.Context, organizationID string, members []workos.Member) (map[string]int, error) {
	workosIDs := make([]string, 0, len(members))
	for _, m := range members {
		workosIDs = append(workosIDs, m.UserID)
	}
	localRows, err := usersrepo.New(s.db).GetConnectedUsersByWorkosIDs(ctx, usersrepo.GetConnectedUsersByWorkosIDsParams{
		WorkosIds:      workosIDs,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("get connected users by workos ids: %w", err)
	}
	localSet := make(map[string]struct{}, len(localRows))
	for _, u := range localRows {
		if u.WorkosID.Valid {
			localSet[u.WorkosID.String] = struct{}{}
		}
	}
	counts := make(map[string]int)
	for _, m := range members {
		if _, ok := localSet[m.UserID]; ok {
			counts[m.RoleSlug]++
		}
	}
	return counts, nil
}

// gramToWorkosIDMap resolves Gram user IDs to WorkOS user IDs.
// Dashboard sends Gram IDs (from ListMembers), but WorkOS membership lookups
// require WorkOS user IDs.
func gramToWorkosIDMap(ctx context.Context, db *pgxpool.Pool, gramIDs []string) (map[string]string, error) {
	users, err := usersrepo.New(db).GetUsersByIDs(ctx, gramIDs)
	if err != nil {
		return nil, fmt.Errorf("get users by ids: %w", err)
	}
	m := make(map[string]string, len(users))
	for _, u := range users {
		if u.WorkosID.Valid && u.WorkosID.String != "" {
			m[u.ID] = u.WorkosID.String
		}
	}
	return m, nil
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
	grants, err := authz.GrantsForRole(ctx, logger, db, organizationID, role.Slug)
	if err != nil {
		return nil, err
	}
	genGrants := make([]*gen.RoleGrant, 0, len(grants))
	for _, g := range grants {
		genGrants = append(genGrants, scopedGrantToGenRoleGrant(g))
	}

	return &gen.Role{
		ID:          role.ID,
		Name:        role.Name,
		Description: role.Description,
		IsSystem:    isSystemRole(role.Slug),
		Grants:      genGrants,
		MemberCount: memberCount,
		CreatedAt:   conv.Default(role.CreatedAt, time.Time{}.UTC().Format(time.RFC3339)),
		UpdatedAt:   conv.Default(role.UpdatedAt, time.Time{}.UTC().Format(time.RFC3339)),
	}, nil
}

func scopedGrantToGenRoleGrant(g *authz.ScopedGrant) *gen.RoleGrant {
	var selectors []*gen.Selector
	for _, sel := range g.Selectors {
		selectors = append(selectors, authzSelectorToGen(sel))
	}
	return &gen.RoleGrant{Scope: g.Scope, Selectors: selectors}
}

// allScopesGrants returns unrestricted grants for every known scope.
// Used when RBAC is not enforced or for admin impersonation.
func allScopesGrants() []*gen.ListRoleGrant {
	return []*gen.ListRoleGrant{
		{Scope: string(authz.ScopeOrgRead), Selectors: nil},
		{Scope: string(authz.ScopeOrgAdmin), Selectors: nil},
		{Scope: string(authz.ScopeProjectRead), Selectors: nil},
		{Scope: string(authz.ScopeProjectWrite), Selectors: nil},
		{Scope: string(authz.ScopeMCPRead), Selectors: nil},
		{Scope: string(authz.ScopeMCPWrite), Selectors: nil},
		{Scope: string(authz.ScopeMCPConnect), Selectors: nil},
	}
}

func listRoleGrantsFromGrants(grants []authz.Grant) []*gen.ListRoleGrant {
	scoped := authz.GrantsToScopedGrants(grants)
	out := make([]*gen.ListRoleGrant, 0, len(scoped))
	for _, g := range scoped {
		var selectors []*gen.Selector
		for _, sel := range g.Selectors {
			selectors = append(selectors, authzSelectorToGen(sel))
		}
		out = append(out, &gen.ListRoleGrant{Scope: g.Scope, SubScopes: g.SubScopes, Selectors: selectors})
	}
	return out
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

func (s *Service) GetRBACStatus(ctx context.Context, _ *gen.GetRBACStatusPayload) (*gen.RBACStatus, error) {
	ac, err := s.requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}

	enabled, err := pfRepo.New(s.db).IsFeatureEnabled(ctx, pfRepo.IsFeatureEnabledParams{
		OrganizationID: ac.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureRBAC),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check RBAC feature flag").Log(ctx, s.logger)
	}

	return &gen.RBACStatus{RbacEnabled: enabled}, nil
}

func (s *Service) EnableRBAC(ctx context.Context, _ *gen.EnableRBACPayload) error {
	ac, err := s.requireSuperAdmin(ctx)
	if err != nil {
		return err
	}
	logger := s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID))

	if err := authz.SeedSystemRoleGrants(ctx, s.logger, s.db, ac.ActiveOrganizationID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "seed system role grants").Log(ctx, logger)
	}

	if _, err := pfRepo.New(s.db).EnableFeature(ctx, pfRepo.EnableFeatureParams{
		OrganizationID: ac.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureRBAC),
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			// Already enabled — unique constraint on (org, feature) WHERE deleted IS FALSE.
			s.featureCache.UpdateFeatureCache(ctx, ac.ActiveOrganizationID, productfeatures.FeatureRBAC, true)
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "enable RBAC feature flag").Log(ctx, logger)
	}

	s.featureCache.UpdateFeatureCache(ctx, ac.ActiveOrganizationID, productfeatures.FeatureRBAC, true)
	return nil
}

func (s *Service) DisableRBAC(ctx context.Context, _ *gen.DisableRBACPayload) error {
	ac, err := s.requireSuperAdmin(ctx)
	if err != nil {
		return err
	}
	logger := s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID))

	if _, err := pfRepo.New(s.db).DeleteFeature(ctx, pfRepo.DeleteFeatureParams{
		OrganizationID: ac.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureRBAC),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already disabled — no active feature row to soft-delete.
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "disable RBAC feature flag").Log(ctx, logger)
	}

	s.featureCache.UpdateFeatureCache(ctx, ac.ActiveOrganizationID, productfeatures.FeatureRBAC, false)
	return nil
}

// requireSuperAdmin returns the auth context and an error if the caller is not
// a Speakeasy employee. Mirrors the exact condition used by the super-admin
// impersonation feature in auth/impl.go: email domain OR admin DB flag.
// Email is read from the auth context (session cache). Admin is read from the
// DB because AuthContext does not carry it; the DB value is synced from the
// Speakeasy provider on every login so it matches the session cache.
func (s *Service) requireSuperAdmin(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}
	email := ""
	if ac.Email != nil {
		email = *ac.Email
	}
	if strings.HasSuffix(email, "@speakeasy.com") || strings.HasSuffix(email, "@speakeasyapi.dev") {
		return ac, nil
	}
	user, err := usersrepo.New(s.db).GetUser(ctx, ac.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get user for admin check").Log(ctx, s.logger)
	}
	if !user.Admin {
		return nil, oops.C(oops.CodeForbidden)
	}
	return ac, nil
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

type challengeUserInfo struct {
	email    string
	photoURL *string
}

func (s *Service) fetchChallengeUserInfo(ctx context.Context, userIDs []string) map[string]challengeUserInfo {
	userMap := make(map[string]challengeUserInfo, len(userIDs))
	if len(userIDs) == 0 {
		return userMap
	}

	users, err := usersrepo.New(s.db).GetUsersByIDs(ctx, userIDs)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to batch-fetch users for challenge enrichment", attr.SlogError(err))
		return userMap
	}

	for _, u := range users {
		var photoURL *string
		if u.PhotoUrl.Valid {
			photoURL = &u.PhotoUrl.String
		}
		userMap[u.ID] = challengeUserInfo{
			email:    u.Email,
			photoURL: photoURL,
		}
	}
	return userMap
}

func (s *Service) ListChallenges(ctx context.Context, payload *gen.ListChallengesPayload) (*gen.ListChallengesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(authCtx.ActiveOrganizationID),
		attr.UserID(authCtx.UserID),
	)

	outcome := ""
	if payload.Outcome != nil {
		outcome = *payload.Outcome
	}
	principalURN := ""
	if payload.PrincipalUrn != nil {
		principalURN = *payload.PrincipalUrn
	}
	scopeFilter := ""
	if payload.Scope != nil {
		scopeFilter = *payload.Scope
	}
	projectID := ""
	if payload.ProjectID != nil {
		projectID = *payload.ProjectID
	}

	// When resolved filter is active, skip CH-side pagination – fetch all matching rows,
	// apply the resolved filter in Go, then slice for the requested page.
	skipPagination := payload.Resolved != nil

	filters := chrepo.ChallengeListFilters{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Outcome:        outcome,
		PrincipalURN:   principalURN,
		Scope:          scopeFilter,
		Limit:          uint64(payload.Limit),  //nolint:gosec // Goa validates 1..200
		Offset:         uint64(payload.Offset), //nolint:gosec // Goa validates >= 0
		SkipPagination: skipPagination,
	}

	chQueries := chrepo.New(s.chConn)

	var total uint64
	if !skipPagination {
		count, err := chQueries.CountChallenges(ctx, filters)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "count challenges from clickhouse").Log(ctx, s.logger)
		}
		if count == 0 {
			return &gen.ListChallengesResult{Challenges: []*gen.AuthzChallenge{}, Total: 0}, nil
		}
		total = count
	}

	challenges, err := chQueries.ListChallenges(ctx, filters)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list challenges from clickhouse").Log(ctx, s.logger)
	}

	if len(challenges) == 0 {
		return &gen.ListChallengesResult{Challenges: []*gen.AuthzChallenge{}, Total: int(total)}, nil
	}

	// Batch-lookup resolutions from PG.
	challengeIDs := make([]string, len(challenges))
	for i, c := range challenges {
		challengeIDs[i] = c.ID
	}

	resolutions, err := repo.New(s.db).ListChallengeResolutions(ctx, repo.ListChallengeResolutionsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ChallengeIds:   challengeIDs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list challenge resolutions").Log(ctx, s.logger)
	}
	resolutionMap := make(map[string]repo.AuthzChallengeResolution, len(resolutions))
	for _, r := range resolutions {
		resolutionMap[r.ChallengeID] = r
	}

	// Apply resolved filter post-join if requested, then paginate in Go
	// (CH-side pagination was skipped so total and page slice are correct).
	if payload.Resolved != nil {
		wantResolved := *payload.Resolved
		filtered := challenges[:0]
		for _, c := range challenges {
			_, hasResolution := resolutionMap[c.ID]
			if hasResolution == wantResolved {
				filtered = append(filtered, c)
			}
		}
		challenges = filtered
		total = uint64(len(challenges))
		offset := uint64(payload.Offset) //nolint:gosec // Goa validates >= 0
		limit := uint64(payload.Limit)   //nolint:gosec // Goa validates 1..200
		if offset >= total {
			challenges = nil
		} else {
			end := min(offset+limit, total)
			challenges = challenges[offset:end]
		}
	}

	// Batch-lookup user photos from PG.
	userIDs := make([]string, 0, len(challenges))
	seen := make(map[string]bool)
	for _, c := range challenges {
		if c.UserID != nil && *c.UserID != "" && !seen[*c.UserID] {
			userIDs = append(userIDs, *c.UserID)
			seen[*c.UserID] = true
		}
	}
	userMap := s.fetchChallengeUserInfo(ctx, userIDs)

	// Build response.
	result := make([]*gen.AuthzChallenge, 0, len(challenges))
	for _, c := range challenges {
		roleSlugs := c.RoleSlugs
		if roleSlugs == nil {
			roleSlugs = []string{}
		}

		var (
			pProjectID      *string
			pResourceKind   *string
			pResourceID     *string
			pUserEmail      *string
			pPhotoURL       *string
			pResolvedAt     *string
			pResolutionType *string
			pResolvedBy     *string
			pResolutionSlug *string
		)

		if c.ProjectID != "" {
			pProjectID = &c.ProjectID
		}
		if c.ResourceKind != "" {
			pResourceKind = &c.ResourceKind
		}
		if c.ResourceID != "" {
			pResourceID = &c.ResourceID
		}

		// Enrich with user data.
		if c.UserID != nil {
			if info, ok := userMap[*c.UserID]; ok {
				pUserEmail = &info.email
				pPhotoURL = info.photoURL
			}
		}
		// Fall back to CH email if PG lookup didn't have it.
		if pUserEmail == nil && c.UserEmail != nil && *c.UserEmail != "" {
			pUserEmail = c.UserEmail
		}

		// Enrich with resolution data.
		if r, ok := resolutionMap[c.ID]; ok {
			resolvedAt := r.CreatedAt.Time.Format(time.RFC3339)
			pResolvedAt = &resolvedAt
			pResolutionType = &r.ResolutionType
			pResolvedBy = &r.ResolvedBy
			if r.RoleSlug.Valid {
				pResolutionSlug = &r.RoleSlug.String
			}
		}

		result = append(result, &gen.AuthzChallenge{
			ID:                  c.ID,
			Timestamp:           c.Timestamp,
			OrganizationID:      c.OrganizationID,
			ProjectID:           pProjectID,
			PrincipalUrn:        c.PrincipalURN,
			PrincipalType:       c.PrincipalType,
			UserEmail:           pUserEmail,
			PhotoURL:            pPhotoURL,
			Operation:           c.Operation,
			Outcome:             c.Outcome,
			Reason:              c.Reason,
			Scope:               c.Scope,
			ResourceKind:        pResourceKind,
			ResourceID:          pResourceID,
			RoleSlugs:           roleSlugs,
			EvaluatedGrantCount: int(c.EvaluatedGrantCount),
			MatchedGrantCount:   int(c.MatchedGrantCount), //nolint:gosec // small number
			ResolvedAt:          pResolvedAt,
			ResolutionType:      pResolutionType,
			ResolvedBy:          pResolvedBy,
			ResolutionRoleSlug:  pResolutionSlug,
		})
	}

	return &gen.ListChallengesResult{
		Challenges: result,
		Total:      int(total),
	}, nil
}

func (s *Service) ResolveChallenge(ctx context.Context, payload *gen.ResolveChallengePayload) (*gen.ResolveChallengesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(authCtx.ActiveOrganizationID),
		attr.UserID(authCtx.UserID),
	)

	if len(payload.ChallengeIds) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "challenge_ids must not be empty").Log(ctx, s.logger)
	}

	// Validate: role_assigned requires role_slug.
	if payload.ResolutionType == "role_assigned" && (payload.RoleSlug == nil || *payload.RoleSlug == "") {
		return nil, oops.E(oops.CodeBadRequest, nil, "role_slug is required when resolution_type is role_assigned").Log(ctx, s.logger)
	}
	if payload.ResolutionType == "dismissed" && payload.RoleSlug != nil && *payload.RoleSlug != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "role_slug must be empty when resolution_type is dismissed").Log(ctx, s.logger)
	}

	resolvedBy := fmt.Sprintf("user:%s", authCtx.UserID)

	resourceKind := ""
	if payload.ResourceKind != nil {
		resourceKind = *payload.ResourceKind
	}
	resourceID := ""
	if payload.ResourceID != nil {
		resourceID = *payload.ResourceID
	}

	rows, err := repo.New(s.db).InsertChallengeResolutions(ctx, repo.InsertChallengeResolutionsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ChallengeIds:   payload.ChallengeIds,
		PrincipalUrn:   payload.PrincipalUrn,
		Scope:          payload.Scope,
		ResourceKind:   resourceKind,
		ResourceID:     resourceID,
		ResolutionType: payload.ResolutionType,
		RoleSlug:       conv.PtrToPGText(payload.RoleSlug),
		ResolvedBy:     resolvedBy,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "insert challenge resolutions").Log(ctx, s.logger)
	}

	resolutions := make([]*gen.ChallengeResolution, 0, len(rows))
	for _, row := range rows {
		resolutions = append(resolutions, &gen.ChallengeResolution{
			ID:             row.ID.String(),
			OrganizationID: row.OrganizationID,
			ChallengeID:    row.ChallengeID,
			PrincipalUrn:   row.PrincipalUrn,
			Scope:          row.Scope,
			ResourceKind:   conv.PtrEmpty(row.ResourceKind),
			ResourceID:     conv.PtrEmpty(row.ResourceID),
			ResolutionType: row.ResolutionType,
			RoleSlug:       conv.FromPGText[string](row.RoleSlug),
			ResolvedBy:     row.ResolvedBy,
			CreatedAt:      row.CreatedAt.Time.Format(time.RFC3339),
		})

		if err := s.audit.LogAccessChallengeResolve(ctx, s.db, audit.LogAccessChallengeResolveEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ChallengeID:      row.ChallengeID,
			PrincipalURN:     row.PrincipalUrn,
			Scope:            row.Scope,
			ResolutionType:   row.ResolutionType,
			RoleSlug:         payload.RoleSlug,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log access challenge resolve").Log(ctx, s.logger)
		}
	}

	return &gen.ResolveChallengesResult{Resolutions: resolutions}, nil
}
