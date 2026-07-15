package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	srv "github.com/speakeasy-api/gram/server/gen/http/access/server"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	chrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/email"
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

// ProductFeatures is the subset of *productfeatures.Client the access service
// needs: enabling RBAC for an org (seed grants + flag, atomically) and keeping
// the feature cache consistent after a direct DB write.
type ProductFeatures interface {
	EnableRBAC(ctx context.Context, organizationID string) error
	UpdateFeatureCache(ctx context.Context, organizationID string, feature productfeatures.Feature, enabled bool)
}

type Service struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	chConn          driver.Conn
	auth            *auth.Auth
	authz           *authz.Engine
	roleMgr         *RoleManager
	productFeatures ProductFeatures
	audit           *audit.Logger
	jwtSecret       string
	accessStore     accesscontrol.Store
	emailSvc        *email.Service
	siteURL         url.URL
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
	productFeatures ProductFeatures,
	auditLogger *audit.Logger,
	jwtSecret string,
	accessStore accesscontrol.Store,
	emailSvc *email.Service,
	siteURL url.URL,
) *Service {
	logger = logger.With(attr.SlogComponent("access"))

	return &Service{
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/access"),
		logger:          logger,
		db:              db,
		chConn:          chConn,
		auth:            auth.New(logger, db, sessions, authz),
		authz:           authz,
		roleMgr:         roleMgr,
		productFeatures: productFeatures,
		audit:           auditLogger,
		jwtSecret:       jwtSecret,
		accessStore:     accessStore,
		emailSvc:        emailSvc,
		siteURL:         siteURL,
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
	if err := s.requireAccessRead(ctx, ac.ActiveOrganizationID); err != nil {
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
	if err := s.requireAccessRead(ctx, ac.ActiveOrganizationID); err != nil {
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
	if err := s.requireManageRoles(ctx, ac.ActiveOrganizationID); err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	created, err := s.roleMgr.CreateRole(ctx, ac.ActiveOrganizationID, workosOrgID, accessAuditActor{
		Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		DisplayName: ac.Email,
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
	if err := s.requireManageRoles(ctx, ac.ActiveOrganizationID); err != nil {
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
	if err := s.requireManageRoles(ctx, ac.ActiveOrganizationID); err != nil {
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
	})
	if err != nil {
		return err
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AccessRoleSlug(deleted.Slug))

	return nil
}

// ListScopes exposes the stable scope catalog so clients can build role editing
// UX without hardcoding permission definitions. Clients should use visibility
// to decide whether a scope is shown directly or only used as storage metadata.
func (s *Service) ListScopes(ctx context.Context, _ *gen.ListScopesPayload) (*gen.ListScopesResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if err := s.requireAccessRead(ctx, ac.ActiveOrganizationID); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	scopes := []scopeDefinitionInput{
		{scope: authz.ScopeOrgRead, description: "Read organization metadata and members.", resourceType: "org"},
		{scope: authz.ScopeOrgBlockedRead, description: "Store exceptions for organization read access.", resourceType: "org"},
		{scope: authz.ScopeOrgAdmin, description: "Manage organization access and settings.", resourceType: "org"},
		{scope: authz.ScopeOrgBlockedAdmin, description: "Store exceptions for organization admin access.", resourceType: "org"},
		{scope: authz.ScopeOrgManageRoles, description: "Create and edit roles, assign roles to members, and manage identity provider (SSO) group-to-role mappings, without access to other organization settings, Observe, or Secure data.", resourceType: "org"},
		{scope: authz.ScopeProjectRead, description: "View projects and project-related resources.", resourceType: "project"},
		{scope: authz.ScopeProjectBlockedRead, description: "Store exceptions for project read access.", resourceType: "project"},
		{scope: authz.ScopeProjectWrite, description: "Create and modify projects and project-related resources.", resourceType: "project"},
		{scope: authz.ScopeProjectBlockedWrite, description: "Store exceptions for project write access.", resourceType: "project"},
		{scope: authz.ScopeMCPRead, description: "View MCP servers and configuration.", resourceType: "mcp"},
		{scope: authz.ScopeMCPBlockedRead, description: "Store exceptions for MCP read access.", resourceType: "mcp"},
		{scope: authz.ScopeMCPWrite, description: "Create and modify MCP servers and configuration.", resourceType: "mcp"},
		{scope: authz.ScopeMCPBlockedWrite, description: "Store exceptions for MCP write access.", resourceType: "mcp"},
		{scope: authz.ScopeMCPConnect, description: "Connect to and use MCP servers.", resourceType: "mcp"},
		{scope: authz.ScopeMCPBlockedConnect, description: "Store exceptions for MCP connect access.", resourceType: "mcp"},
		{scope: authz.ScopeEnvironmentRead, description: "View environments and their entries within the project.", resourceType: "environment"},
		{scope: authz.ScopeEnvironmentBlockedRead, description: "Store exceptions for environment read access.", resourceType: "environment"},
		{scope: authz.ScopeEnvironmentWrite, description: "Add, edit, clone, and remove environments within the project.", resourceType: "environment"},
		{scope: authz.ScopeEnvironmentBlockedWrite, description: "Store exceptions for environment write access.", resourceType: "environment"},
		{scope: authz.ScopeSkillRead, description: "View skills within the project.", resourceType: "skill"},
		{scope: authz.ScopeSkillBlockedRead, description: "Store exceptions for skill read access.", resourceType: "skill"},
		{scope: authz.ScopeSkillWrite, description: "Create and modify skills within the project.", resourceType: "skill"},
		{scope: authz.ScopeSkillBlockedWrite, description: "Store exceptions for skill write access.", resourceType: "skill"},
		{scope: authz.ScopeRiskPolicyEvaluate, description: "Evaluate risk policies.", resourceType: "risk_policy"},
		{scope: authz.ScopeRiskPolicyBypass, description: "Bypass risk policies.", resourceType: "risk_policy"},
		{scope: authz.ScopeChatRead, description: "Read every member's agent session transcripts and reveal the secret values flagged in Risk Events. Members can always read their own sessions, no one else's; this grant adds access to everyone else's sessions and to unmasking flagged secrets.", resourceType: "chat"},
	}
	result := make([]*gen.ScopeDefinition, 0, len(scopes))
	for _, scope := range scopes {
		result = append(result, scopeDefinition(scope))
	}

	return &gen.ListScopesResult{Scopes: result}, nil
}

type scopeDefinitionInput struct {
	scope        authz.Scope
	description  string
	resourceType string
}

func scopeDefinition(input scopeDefinitionInput) *gen.ScopeDefinition {
	var exclusionScope *string
	if exclusion, ok := authz.ExclusionScopeFor(input.scope); ok {
		exclusionScopeValue := string(exclusion)
		exclusionScope = &exclusionScopeValue
	}
	visibility, ok := authz.ScopeVisibilityFor(input.scope)
	if !ok {
		visibility = authz.ScopeVisibilityInternal
	}

	return &gen.ScopeDefinition{
		Slug:           string(input.scope),
		Description:    input.description,
		ResourceType:   input.resourceType,
		Visibility:     visibility,
		ExclusionScope: exclusionScope,
	}
}

// ListMembers follows the original access API contract by returning WorkOS user
// identifiers while decorating them with the role information the UI needs.
func (s *Service) ListMembers(ctx context.Context, _ *gen.ListMembersPayload) (*gen.ListMembersResult, error) {
	ac, _, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireAccessRead(ctx, ac.ActiveOrganizationID); err != nil {
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
		return &gen.ListUserGrantsResult{Grants: userVisibleScopeGrants()}, nil
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
			return &gen.ListUserGrantsResult{Grants: userVisibleScopeGrants()}, nil
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

	principals, err := authz.ResolveUserPrincipals(ctx, s.db, ac.ActiveOrganizationID, ac.UserID)
	switch {
	case errors.Is(err, authz.ErrPrincipalInvalid):
		return nil, oops.E(oops.CodeUnauthorized, err, "invalid user principal").LogError(ctx, logger)
	case errors.Is(err, authz.ErrPrincipalNotFound):
		return nil, oops.E(oops.CodeNotFound, nil, "current user has not joined this organization").LogError(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "resolve user principals").LogError(ctx, logger)
	}

	grants, err := authz.LoadGrants(ctx, s.db, ac.ActiveOrganizationID, principals)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load effective user grants").LogError(ctx, logger)
	}

	return &gen.ListUserGrantsResult{Grants: listRoleGrantsFromGrants(grants)}, nil
}

// UpdateMemberRoles replaces all role assignments for a member. It is
// intentionally stricter than member listing: it only mutates access for users
// Gram knows are connected to the local organization.
func (s *Service) UpdateMemberRoles(ctx context.Context, payload *gen.UpdateMemberRolesPayload) (*gen.AccessMember, error) {
	ac, _, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireManageRoles(ctx, ac.ActiveOrganizationID); err != nil {
		return nil, err
	}
	if len(payload.RoleIds) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "at least one role is required").LogError(ctx, s.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessMemberID(payload.UserID),
	)

	memberUpdate, err := s.roleMgr.UpdateMemberRoles(ctx, ac.ActiveOrganizationID, payload.UserID, payload.RoleIds, accessAuditActor{
		Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		DisplayName: ac.Email,
	})
	if err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
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
		return nil, "", oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}

	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, "", oops.E(oops.CodeUnexpected, err, "get organization metadata").LogError(ctx, s.logger)
	}
	if !org.WorkosID.Valid || org.WorkosID.String == "" {
		return nil, "", oops.E(oops.CodeBadRequest, nil, "organization is not linked to WorkOS").LogError(ctx, s.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	return ac, org.WorkosID.String, nil
}

// requireAccessRead gates read-only access-management endpoints (roles,
// members, scopes, authorization challenges). Either org:read (any org viewer)
// or org:manage_roles (the dedicated access-management role) may read this data.
func (s *Service) requireAccessRead(ctx context.Context, orgID string) error {
	return s.authz.RequireAny(ctx,
		authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: orgID, Dimensions: nil},
		authz.Check{Scope: authz.ScopeOrgManageRoles, ResourceKind: "", ResourceID: orgID, Dimensions: nil},
	)
}

// requireManageRoles gates access-management mutations (role CRUD, member role
// assignment, challenge resolution). org:manage_roles is the dedicated scope;
// org:admin satisfies it via scope expansion, so full org admins are unaffected.
func (s *Service) requireManageRoles(ctx context.Context, orgID string) error {
	return s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgManageRoles, ResourceKind: "", ResourceID: orgID, Dimensions: nil})
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
			Effect:    authz.PolicyEffectAllow,
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
	if s.ServerURL != nil {
		sel["server_url"] = *s.ServerURL
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
		ServerURL:    nil,
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
	if v, ok := sel["server_url"]; ok {
		s.ServerURL = &v
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

// userVisibleScopeGrants returns unrestricted grants for every first-class
// permission scope. Used when RBAC is not enforced or for admin impersonation.
func userVisibleScopeGrants() []*gen.ListRoleGrant {
	return []*gen.ListRoleGrant{
		{Scope: string(authz.ScopeOrgRead), Selectors: nil},
		{Scope: string(authz.ScopeOrgAdmin), Selectors: nil},
		{Scope: string(authz.ScopeOrgManageRoles), Selectors: nil},
		{Scope: string(authz.ScopeProjectRead), Selectors: nil},
		{Scope: string(authz.ScopeProjectWrite), Selectors: nil},
		{Scope: string(authz.ScopeMCPRead), Selectors: nil},
		{Scope: string(authz.ScopeMCPWrite), Selectors: nil},
		{Scope: string(authz.ScopeMCPConnect), Selectors: nil},
		{Scope: string(authz.ScopeEnvironmentRead), Selectors: nil},
		{Scope: string(authz.ScopeEnvironmentWrite), Selectors: nil},
		{Scope: string(authz.ScopeSkillRead), Selectors: nil},
		{Scope: string(authz.ScopeSkillWrite), Selectors: nil},
		{Scope: string(authz.ScopeRiskPolicyEvaluate), Selectors: nil},
		{Scope: string(authz.ScopeRiskPolicyBypass), Selectors: nil},
		{Scope: string(authz.ScopeChatRead), Selectors: nil},
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
		UserID:         conv.ToPGText(userID),
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
	ac, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	enabled, err := pfRepo.New(s.db).IsFeatureEnabled(ctx, pfRepo.IsFeatureEnabledParams{
		OrganizationID: ac.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureRBAC),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check RBAC feature flag").LogError(ctx, s.logger)
	}

	return &gen.RBACStatus{RbacEnabled: enabled}, nil
}

func (s *Service) EnableRBAC(ctx context.Context, _ *gen.EnableRBACPayload) error {
	ac, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return err
	}
	if err := s.productFeatures.EnableRBAC(ctx, ac.ActiveOrganizationID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "enable RBAC").LogError(ctx, s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID)))
	}

	return nil
}

func (s *Service) DisableRBAC(ctx context.Context, _ *gen.DisableRBACPayload) error {
	ac, err := s.requirePlatformAdmin(ctx)
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
		return oops.E(oops.CodeUnexpected, err, "disable RBAC feature flag").LogError(ctx, logger)
	}

	s.productFeatures.UpdateFeatureCache(ctx, ac.ActiveOrganizationID, productfeatures.FeatureRBAC, false)
	return nil
}

// requirePlatformAdmin returns the auth context and an error if the caller is not
// a Speakeasy employee. Mirrors the exact condition used by the platform-admin
// impersonation feature in auth/impl.go: email domain OR admin DB flag.
// Email is read from the auth context (session cache). Admin is read from the
// DB because AuthContext does not carry it; the DB value is synced from the
// Speakeasy provider on every login so it matches the session cache.
func (s *Service) requirePlatformAdmin(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "get user for admin check").LogError(ctx, s.logger)
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

// activeOrgMemberUserIDs returns the Gram user IDs of active members of the
// organization. The challenge UI uses it to suppress challenges raised by users
// outside the organization — e.g. Speakeasy staff impersonating a customer org,
// whose entries otherwise clutter the list while they switch accounts. Always
// returns a non-nil slice so callers unconditionally apply the suppression.
func (s *Service) activeOrgMemberUserIDs(ctx context.Context, orgID string) ([]string, error) {
	ids, err := orgrepo.New(s.db).ListActiveOrganizationUserIDs(ctx, orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list active org member ids").LogError(ctx, s.logger)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
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
	if err := s.requireAccessRead(ctx, authCtx.ActiveOrganizationID); err != nil {
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
			return nil, oops.E(oops.CodeUnexpected, err, "list challenges by ids from clickhouse").LogError(ctx, s.logger)
		}
		return s.buildChallengeResult(ctx, authCtx, challenges, len(challenges))
	}

	// When resolved filter is active, skip CH-side pagination – fetch all matching rows,
	// apply the resolved filter in Go, then slice for the requested page.
	skipPagination := payload.Resolved != nil

	// Suppress challenges from users outside the org so counts and pagination
	// stay correct (filtering happens in ClickHouse, before grouping/paging).
	memberIDs, err := s.activeOrgMemberUserIDs(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	filters := chrepo.ChallengeListFilters{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      payload.ProjectID,
		Outcome:        payload.Outcome,
		PrincipalURN:   payload.PrincipalUrn,
		Scope:          payload.Scope,
		Limit:          uint64(payload.Limit),  //nolint:gosec // Goa validates 1..200
		Offset:         uint64(payload.Offset), //nolint:gosec // Goa validates >= 0
		SkipPagination: skipPagination,
		MemberUserIDs:  memberIDs,
	}

	var total uint64
	if !skipPagination {
		count, err := chQueries.CountChallenges(ctx, filters)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "count challenges from clickhouse").LogError(ctx, s.logger)
		}
		if count == 0 {
			return &gen.ListChallengesResult{Challenges: []*gen.AuthzChallenge{}, Total: 0}, nil
		}
		total = count
	}

	challenges, err := chQueries.ListChallenges(ctx, filters)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list challenges from clickhouse").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list challenge resolutions").LogError(ctx, s.logger)
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
	if err := s.requireAccessRead(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(authCtx.ActiveOrganizationID),
		attr.UserID(authCtx.UserID),
	)

	skipPagination := payload.Resolved != nil

	// Suppress challenges from users outside the org (see activeOrgMemberUserIDs).
	memberIDs, err := s.activeOrgMemberUserIDs(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	filters := chrepo.ChallengeListFilters{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      payload.ProjectID,
		Outcome:        payload.Outcome,
		PrincipalURN:   payload.PrincipalUrn,
		Scope:          payload.Scope,
		Limit:          uint64(payload.Limit),  //nolint:gosec // Goa validates 1..200
		Offset:         uint64(payload.Offset), //nolint:gosec // Goa validates >= 0
		SkipPagination: skipPagination,
		MemberUserIDs:  memberIDs,
	}

	chQueries := chrepo.New(s.chConn)

	var total uint64
	if !skipPagination {
		count, err := chQueries.CountChallengeBuckets(ctx, filters)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "count challenge buckets from clickhouse").LogError(ctx, s.logger)
		}
		if count == 0 {
			return &gen.ListChallengeBucketsResult{Buckets: []*gen.ChallengeBucket{}, Total: 0}, nil
		}
		total = count
	}

	buckets, err := chQueries.ListChallengeBuckets(ctx, filters)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list challenge buckets from clickhouse").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list challenge resolutions").LogError(ctx, s.logger)
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
	if err := s.requireManageRoles(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(authCtx.ActiveOrganizationID),
		attr.UserID(authCtx.UserID),
	)

	if len(payload.ChallengeIds) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "challenge_ids must not be empty").LogError(ctx, s.logger)
	}

	// Validate: role_assigned requires role_slug.
	if payload.ResolutionType == "role_assigned" && (payload.RoleSlug == nil || *payload.RoleSlug == "") {
		return nil, oops.E(oops.CodeBadRequest, nil, "role_slug is required when resolution_type is role_assigned").LogError(ctx, s.logger)
	}
	if payload.ResolutionType == "dismissed" && payload.RoleSlug != nil && *payload.RoleSlug != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "role_slug must be empty when resolution_type is dismissed").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "insert challenge resolutions").LogError(ctx, s.logger)
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
			return nil, oops.E(oops.CodeUnexpected, err, "log access challenge resolve").LogError(ctx, s.logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, s.logger)
	}

	return &gen.ResolveChallengesResult{Resolutions: resolutions}, nil
}
