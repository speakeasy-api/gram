package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	pfRepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var errConnectedUserNotFound = errors.New("connected user not found")

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
	roleMgr      *RoleManager
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
	roleMgr *RoleManager,
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
		roleMgr:      roleMgr,
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

// ListRoles reads local role records and enriches them with Gram's local grant state.
func (s *Service) ListRoles(ctx context.Context, _ *gen.ListRolesPayload) (*gen.ListRolesResult, error) {
	ac, _, err := s.roleOrgContext(ctx)
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

	return s.roleMgr.ListRoles(ctx, ac.ActiveOrganizationID)
}

// GetRole returns the role definition enriched with Gram's local grant
// state so callers see the complete effective role configuration in one place.
func (s *Service) GetRole(ctx context.Context, payload *gen.GetRolePayload) (*gen.Role, error) {
	ac, _, err := s.roleOrgContext(ctx)
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

	return s.roleMgr.GetRoleByID(ctx, ac.ActiveOrganizationID, payload.ID)
}

func (s *Service) CreateRole(ctx context.Context, payload *gen.CreateRolePayload) (*gen.Role, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	created, err := s.roleMgr.CreateRole(ctx, ac.ActiveOrganizationID, workosOrgID, accessAuditActor{
		Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		DisplayName: ac.Email,
		Slug:        nil,
	}, payload)
	if err != nil {
		return nil, err
	}

	return created.Role, nil
}

func (s *Service) UpdateRole(ctx context.Context, payload *gen.UpdateRolePayload) (*gen.Role, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessRoleID(payload.ID),
	)

	updated, err := s.roleMgr.UpdateRole(ctx, ac.ActiveOrganizationID, workosOrgID, accessAuditActor{
		Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		DisplayName: ac.Email,
		Slug:        nil,
	}, payload)
	if err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSlug(updated.Role.Slug))

	return updated.After, nil
}

func (s *Service) DeleteRole(ctx context.Context, payload *gen.DeleteRolePayload) error {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessRoleID(payload.ID),
	)

	deleted, err := s.roleMgr.DeleteRole(ctx, ac.ActiveOrganizationID, workosOrgID, payload.ID, accessAuditActor{
		Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		DisplayName: ac.Email,
		Slug:        nil,
	})
	if err != nil {
		return err
	}
	deletedRole := deleted.Role
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSlug(deletedRole.Slug))

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
		{Slug: string(authz.ScopeEnvironmentRead), Description: "View environments and their entries within the project.", ResourceType: "environment"},
		{Slug: string(authz.ScopeEnvironmentWrite), Description: "Add, edit, clone, and remove environments within the project.", ResourceType: "environment"},
	}}, nil
}

// ListMembers follows the original access API contract by returning WorkOS user
// identifiers while decorating them with the role information the UI needs.
func (s *Service) ListMembers(ctx context.Context, _ *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	ac, _, err := s.roleOrgContext(ctx)
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

	return s.roleMgr.ListMembers(ctx, ac.ActiveOrganizationID)
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

	ac, _, err := s.roleOrgContext(ctx)
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
	roleSlugs, err := s.roleMgr.MemberRoleSlugs(ctx, ac.ActiveOrganizationID, connectedUser.WorkosID.String)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list member roles").Log(ctx, logger)
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
	ac, _, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessMemberID(payload.UserID),
		attr.AccessRoleID(payload.RoleID),
	)

	memberUpdate, err := s.roleMgr.UpdateMemberRole(ctx, ac.ActiveOrganizationID, payload.UserID, payload.RoleID, accessAuditActor{
		Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		DisplayName: ac.Email,
		Slug:        nil,
	})
	if err != nil {
		return nil, err
	}
	roleSlug := memberUpdate.RoleSlug
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessRoleSlug(roleSlug),
	)

	return memberUpdate.After, nil
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
	if s.ProjectID != nil {
		sel["project_id"] = *s.ProjectID
	}
	return sel
}

func authzSelectorToGen(sel authz.Selector) *gen.Selector {
	s := &gen.Selector{
		ResourceKind: sel["resource_kind"],
		ResourceID:   sel["resource_id"],
		Disposition:  nil,
		Tool:         nil,
		ProjectID:    nil,
	}
	if v, ok := sel["disposition"]; ok {
		s.Disposition = &v
	}
	if v, ok := sel["tool"]; ok {
		s.Tool = &v
	}
	if v, ok := sel["project_id"]; ok {
		s.ProjectID = &v
	}
	return s
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
		{Scope: string(authz.ScopeEnvironmentRead), Selectors: nil},
		{Scope: string(authz.ScopeEnvironmentWrite), Selectors: nil},
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

func connectedUser(ctx context.Context, db database.DBTX, organizationID string, userID string) (usersrepo.User, error) {
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

	chQueries := chrepo.New(s.chConn)

	// Fast path: fetch specific challenges by ID (used for bucket expansion).
	if len(payload.Ids) > 0 {
		challenges, err := chQueries.ListChallengesByIDs(ctx, authCtx.ActiveOrganizationID, payload.Ids)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list challenges by ids from clickhouse").Log(ctx, s.logger)
		}
		return s.buildChallengeResult(ctx, authCtx, challenges, len(challenges))
	}

	// When resolved filter is active, skip CH-side pagination – fetch all matching rows,
	// apply the resolved filter in Go, then slice for the requested page.
	skipPagination := payload.Resolved != nil

	filters := chrepo.ChallengeListFilters{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      payload.ProjectID,
		Outcome:        payload.Outcome,
		PrincipalURN:   payload.PrincipalUrn,
		Scope:          payload.Scope,
		Limit:          uint64(payload.Limit),  //nolint:gosec // Goa validates 1..200
		Offset:         uint64(payload.Offset), //nolint:gosec // Goa validates >= 0
		SkipPagination: skipPagination,
	}

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

	// Apply resolved filter post-join if requested, then paginate in Go
	// (CH-side pagination was skipped so total and page slice are correct).
	if payload.Resolved != nil {
		resolutions, err := s.lookupResolutions(ctx, authCtx.ActiveOrganizationID, challenges)
		if err != nil {
			return nil, err
		}
		wantResolved := *payload.Resolved
		filtered := challenges[:0]
		for _, c := range challenges {
			_, hasResolution := resolutions[c.ID]
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

	return s.buildChallengeResult(ctx, authCtx, challenges, int(total))
}

// lookupResolutions batch-fetches resolution state from PG for a set of CH challenges.
func (s *Service) lookupResolutions(ctx context.Context, orgID string, challenges []chrepo.ChallengeSummary) (map[string]repo.AuthzChallengeResolution, error) {
	ids := make([]string, len(challenges))
	for i, c := range challenges {
		ids[i] = c.ID
	}
	resolutions, err := repo.New(s.db).ListChallengeResolutions(ctx, repo.ListChallengeResolutionsParams{
		OrganizationID: orgID,
		ChallengeIds:   ids,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list challenge resolutions").Log(ctx, s.logger)
	}
	m := make(map[string]repo.AuthzChallengeResolution, len(resolutions))
	for _, r := range resolutions {
		m[r.ChallengeID] = r
	}
	return m, nil
}

// buildChallengeResult enriches CH challenge summaries with PG resolution + user data and builds the API response.
func (s *Service) buildChallengeResult(ctx context.Context, authCtx *contextvalues.AuthContext, challenges []chrepo.ChallengeSummary, total int) (*gen.ListChallengesResult, error) {
	if len(challenges) == 0 {
		return &gen.ListChallengesResult{Challenges: []*gen.AuthzChallenge{}, Total: total}, nil
	}

	resolutionMap, err := s.lookupResolutions(ctx, authCtx.ActiveOrganizationID, challenges)
	if err != nil {
		return nil, err
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
		Total:      total,
	}, nil
}

func (s *Service) ListChallengeBuckets(ctx context.Context, payload *gen.ListChallengeBucketsPayload) (*gen.ListChallengeBucketsResult, error) {
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

	skipPagination := payload.Resolved != nil

	filters := chrepo.ChallengeListFilters{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      payload.ProjectID,
		Outcome:        payload.Outcome,
		PrincipalURN:   payload.PrincipalUrn,
		Scope:          payload.Scope,
		Limit:          uint64(payload.Limit),  //nolint:gosec // Goa validates 1..200
		Offset:         uint64(payload.Offset), //nolint:gosec // Goa validates >= 0
		SkipPagination: skipPagination,
	}

	chQueries := chrepo.New(s.chConn)

	var total uint64
	if !skipPagination {
		count, err := chQueries.CountChallengeBuckets(ctx, filters)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "count challenge buckets from clickhouse").Log(ctx, s.logger)
		}
		if count == 0 {
			return &gen.ListChallengeBucketsResult{Buckets: []*gen.ChallengeBucket{}, Total: 0}, nil
		}
		total = count
	}

	buckets, err := chQueries.ListChallengeBuckets(ctx, filters)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list challenge buckets from clickhouse").Log(ctx, s.logger)
	}

	if len(buckets) == 0 {
		return &gen.ListChallengeBucketsResult{Buckets: []*gen.ChallengeBucket{}, Total: int(total)}, nil
	}

	// Batch-lookup resolutions from PG using all challenge IDs across buckets.
	allChallengeIDs := make([]string, 0, len(buckets))
	for _, b := range buckets {
		allChallengeIDs = append(allChallengeIDs, b.ChallengeIDs...)
	}

	resolutions, err := repo.New(s.db).ListChallengeResolutions(ctx, repo.ListChallengeResolutionsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ChallengeIds:   allChallengeIDs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list challenge resolutions").Log(ctx, s.logger)
	}
	resolutionMap := make(map[string]repo.AuthzChallengeResolution, len(resolutions))
	for _, r := range resolutions {
		resolutionMap[r.ChallengeID] = r
	}

	// Apply resolved filter post-join if requested, then paginate in Go.
	if payload.Resolved != nil {
		wantResolved := *payload.Resolved
		filtered := buckets[:0]
		for _, b := range buckets {
			_, hasResolution := resolutionMap[b.ID]
			if hasResolution == wantResolved {
				filtered = append(filtered, b)
			}
		}
		buckets = filtered
		total = uint64(len(buckets))
		offset := uint64(payload.Offset) //nolint:gosec // Goa validates >= 0
		limit := uint64(payload.Limit)   //nolint:gosec // Goa validates 1..200
		if offset >= total {
			buckets = nil
		} else {
			end := min(offset+limit, total)
			buckets = buckets[offset:end]
		}
	}

	// Batch-lookup user photos from PG.
	userIDs := make([]string, 0, len(buckets))
	seen := make(map[string]bool)
	for _, b := range buckets {
		if b.UserID != nil && *b.UserID != "" && !seen[*b.UserID] {
			userIDs = append(userIDs, *b.UserID)
			seen[*b.UserID] = true
		}
	}
	userMap := s.fetchChallengeUserInfo(ctx, userIDs)

	// Build response.
	result := make([]*gen.ChallengeBucket, 0, len(buckets))
	for _, b := range buckets {
		roleSlugs := b.RoleSlugs
		if roleSlugs == nil {
			roleSlugs = []string{}
		}
		challengeIDs := b.ChallengeIDs
		if challengeIDs == nil {
			challengeIDs = []string{}
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

		if b.ProjectID != "" {
			pProjectID = &b.ProjectID
		}
		if b.ResourceKind != "" {
			pResourceKind = &b.ResourceKind
		}
		if b.ResourceID != "" {
			pResourceID = &b.ResourceID
		}

		// Enrich with user data.
		if b.UserID != nil {
			if info, ok := userMap[*b.UserID]; ok {
				pUserEmail = &info.email
				pPhotoURL = info.photoURL
			}
		}
		if pUserEmail == nil && b.UserEmail != nil && *b.UserEmail != "" {
			pUserEmail = b.UserEmail
		}

		// Enrich with resolution data (use the representative challenge's ID).
		if r, ok := resolutionMap[b.ID]; ok {
			resolvedAt := r.CreatedAt.Time.Format(time.RFC3339)
			pResolvedAt = &resolvedAt
			pResolutionType = &r.ResolutionType
			pResolvedBy = &r.ResolvedBy
			if r.RoleSlug.Valid {
				pResolutionSlug = &r.RoleSlug.String
			}
		}

		result = append(result, &gen.ChallengeBucket{
			ID:                  b.ID,
			LastSeen:            b.LastSeen,
			FirstSeen:           b.FirstSeen,
			OrganizationID:      b.OrganizationID,
			ProjectID:           pProjectID,
			PrincipalUrn:        b.PrincipalURN,
			PrincipalType:       b.PrincipalType,
			UserEmail:           pUserEmail,
			PhotoURL:            pPhotoURL,
			Operation:           b.Operation,
			Outcome:             b.Outcome,
			Reason:              b.Reason,
			Scope:               b.Scope,
			ResourceKind:        pResourceKind,
			ResourceID:          pResourceID,
			RoleSlugs:           roleSlugs,
			EvaluatedGrantCount: int(b.EvaluatedGrantCount),
			MatchedGrantCount:   int(b.MatchedGrantCount), //nolint:gosec // small number
			ChallengeCount:      int(b.ChallengeCount),    //nolint:gosec // small number
			ChallengeIds:        challengeIDs,
			ResolvedAt:          pResolvedAt,
			ResolutionType:      pResolutionType,
			ResolvedBy:          pResolvedBy,
			ResolutionRoleSlug:  pResolutionSlug,
		})
	}

	return &gen.ListChallengeBucketsResult{
		Buckets: result,
		Total:   int(total),
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

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	rows, err := repo.New(dbtx).InsertChallengeResolutions(ctx, repo.InsertChallengeResolutionsParams{
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

		if err := s.audit.LogAccessChallengeResolve(ctx, dbtx, audit.LogAccessChallengeResolveEvent{
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

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, s.logger)
	}

	return &gen.ResolveChallengesResult{Resolutions: resolutions}, nil
}
