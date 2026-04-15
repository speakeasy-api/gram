package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type FeatureChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// MembershipFetcher retrieves a WorkOS membership for a user+org pair.
type MembershipFetcher interface {
	GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*workos.Member, error)
}

type ManagerOpts struct {
	DevMode bool
}

// roleSlugCache is the Redis cache entry for a resolved role slug.
// Key is org-first so DeleteByPrefix on "role-slug:{orgID}:" invalidates the whole org.
type roleSlugCache struct {
	UserID string
	OrgID  string
	Slug   string
}

var _ cache.CacheableObject[roleSlugCache] = (*roleSlugCache)(nil)

func (r roleSlugCache) CacheKey() string {
	return "role-slug:" + r.OrgID + ":" + r.UserID
}

func (r roleSlugCache) TTL() time.Duration {
	return 5 * time.Minute
}

func (r roleSlugCache) AdditionalCacheKeys() []string {
	return nil
}

type Manager struct {
	logger     *slog.Logger
	db         accessrepo.DBTX
	features   FeatureChecker
	isDev      bool
	membership MembershipFetcher
	roleCache  cache.TypedCacheObject[roleSlugCache]
}

func NewManager(logger *slog.Logger, db accessrepo.DBTX, features FeatureChecker, membership MembershipFetcher, roleCache cache.Cache, opts ...ManagerOpts) *Manager {
	var devMode bool
	if len(opts) > 0 {
		devMode = opts[0].DevMode
	}

	return &Manager{
		logger:     logger.With(attr.SlogComponent("access")),
		db:         db,
		features:   features,
		isDev:      devMode,
		membership: membership,
		roleCache:  cache.NewTypedObjectCache[roleSlugCache](logger.With(attr.SlogCacheNamespace("access-role-slug")), roleCache, cache.SuffixNone),
	}
}

// getScopeOverrides returns the parsed scope overrides from the request context
// if they are present AND the caller is authorised to use them. In local dev
// any authenticated user may use the override header; in production only
// superadmins can. Returns nil, false when overrides are absent or disallowed.
func (m *Manager) getScopeOverrides(ctx context.Context) ([]RoleGrant, bool) {
	overrides, ok := readScopeOverrides(ctx)
	if !ok {
		return nil, false
	}
	if m.isDev {
		return overrides, true
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || !authCtx.IsAdmin {
		return nil, false
	}

	return overrides, true
}

func (m *Manager) PrepareContext(ctx context.Context) (context.Context, error) {
	if grants, ok := GrantsFromContext(ctx); ok && grants != nil {
		return ctx, nil
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return ctx, nil
	}

	if overrides, ok := m.getScopeOverrides(ctx); ok {
		grants := grantsFromOverrides(overrides)
		return GrantsToContext(ctx, grants), nil
	}

	if authCtx.AccountType != "enterprise" {
		return ctx, nil
	}

	enabled, err := m.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureRBAC)
	if err != nil {
		m.logger.WarnContext(ctx, "failed to check RBAC feature flag, skipping grant loading",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogError(err),
		)
		return ctx, nil
	}
	if !enabled {
		return ctx, nil
	}

	principals := []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)}

	roleSlug, err := m.resolveRoleSlug(ctx, authCtx.UserID, authCtx.ActiveOrganizationID)
	if err != nil {
		m.logger.ErrorContext(
			ctx,
			"failed to resolve role for access grants",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogUserID(authCtx.UserID),
			attr.SlogError(err),
		)
		return ctx, fmt.Errorf("resolve role slug: %w", err)
	}
	if roleSlug != "" {
		principals = append(principals, urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug))
	}

	grants, err := LoadGrants(ctx, m.db, authCtx.ActiveOrganizationID, principals)
	if err != nil {
		m.logger.ErrorContext(
			ctx,
			"failed to load access grants",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogUserID(authCtx.UserID),
			attr.SlogError(err),
		)
		return ctx, fmt.Errorf("load access grants: %w", err)
	}

	return GrantsToContext(ctx, grants), nil
}

func (m *Manager) resolveRoleSlug(ctx context.Context, userID, orgID string) (string, error) {
	cacheKey := roleSlugCache{UserID: userID, OrgID: orgID, Slug: ""}.CacheKey()
	if cached, err := m.roleCache.Get(ctx, cacheKey); err == nil {
		return cached.Slug, nil
	}

	user, err := usersrepo.New(m.db).GetUser(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	if !user.WorkosID.Valid || user.WorkosID.String == "" {
		return "", nil
	}

	org, err := orgrepo.New(m.db).GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("get org: %w", err)
	}
	if !org.WorkosID.Valid || org.WorkosID.String == "" {
		return "", nil
	}

	member, err := m.membership.GetOrgMembership(ctx, user.WorkosID.String, org.WorkosID.String)
	if err != nil {
		return "", fmt.Errorf("get org membership: %w", err)
	}
	if member == nil {
		return "", nil
	}

	entry := roleSlugCache{UserID: userID, OrgID: orgID, Slug: member.RoleSlug}
	if err := m.roleCache.Store(ctx, entry); err != nil {
		m.logger.WarnContext(ctx, "failed to cache role slug",
			attr.SlogUserID(userID),
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
	}

	return member.RoleSlug, nil
}

// InvalidateRoleCache removes the cached role slug for a single user. Call
// this after updating a specific member's role via UpdateMemberRole.
func (m *Manager) InvalidateRoleCache(ctx context.Context, userID, orgID string) {
	entry := roleSlugCache{UserID: userID, OrgID: orgID, Slug: ""}
	if err := m.roleCache.Delete(ctx, entry); err != nil {
		m.logger.WarnContext(ctx, "failed to invalidate cached role slug",
			attr.SlogUserID(userID),
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
	}
}

// InvalidateAllRoleCaches removes all cached role slugs for an org. Call this
// after bulk role reassignments where individual user IDs aren't tracked.
func (m *Manager) InvalidateAllRoleCaches(ctx context.Context, orgID string) {
	if err := m.roleCache.DeleteByPrefix(ctx, "role-slug:"+orgID+":"); err != nil {
		m.logger.WarnContext(ctx, "failed to invalidate cached role slugs for org",
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
	}
}

func (m *Manager) Require(ctx context.Context, checks ...Check) error {
	enforce, err := m.shouldEnforce(ctx)
	if err != nil {
		return err
	}
	if !enforce {
		return nil
	}
	if len(checks) == 0 {
		return m.mapError(ctx, ErrNoChecks)
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok || grants == nil {
		return m.mapError(ctx, ErrMissingGrants)
	}

	for _, check := range checks {
		if err := validateInput(check); err != nil {
			return m.mapError(ctx, err)
		}

		if !grants.satisfies(check.expand()) {
			return m.mapError(ctx, Denied(check.Scope, check.ResourceID))
		}
	}

	return nil
}

func (m *Manager) RequireAny(ctx context.Context, checks ...Check) error {
	enforce, err := m.shouldEnforce(ctx)
	if err != nil {
		return err
	}
	if !enforce {
		return nil
	}
	if len(checks) == 0 {
		return m.mapError(ctx, ErrNoChecks)
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok || grants == nil {
		return m.mapError(ctx, ErrMissingGrants)
	}

	for _, check := range checks {
		if err := validateInput(check); err != nil {
			return m.mapError(ctx, err)
		}
	}

	if slices.ContainsFunc(checks, func(c Check) bool { return grants.satisfies(c.expand()) }) {
		return nil
	}

	return m.mapError(ctx, Denied(checks[0].Scope, checks[0].ResourceID))
}

func (m *Manager) Filter(ctx context.Context, scope Scope, resourceIDs []string) ([]string, error) {
	enforce, err := m.shouldEnforce(ctx)
	if err != nil {
		return nil, err
	}
	if !enforce {
		return resourceIDs, nil
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok || grants == nil {
		return nil, m.mapError(ctx, ErrMissingGrants)
	}

	allowed := make([]string, 0, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		if err := validateInput(Check{Scope: scope, ResourceID: resourceID}); err != nil {
			return nil, m.mapError(ctx, err)
		}

		if grants.satisfies(Check{Scope: scope, ResourceID: resourceID}.expand()) {
			allowed = append(allowed, resourceID)
		}
	}

	return allowed, nil
}

func (m *Manager) shouldEnforce(ctx context.Context) (bool, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return false, oops.C(oops.CodeUnauthorized)
	}

	// Never enforce RBAC on API key requests — they have their own scoping.
	if authCtx.APIKeyID != "" {
		return false, nil
	}

	// When the caller has active scope overrides, enforce so the override scopes
	// take effect regardless of account type or feature flag. Checked after
	// API key exclusion so the toolbar doesn't interfere with API key auth flows.
	if _, ok := m.getScopeOverrides(ctx); ok {
		return true, nil
	}

	if authCtx.AccountType != "enterprise" || authCtx.SessionID == nil {
		return false, nil
	}

	enabled, err := m.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureRBAC)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "check RBAC feature").Log(ctx, m.logger)
	}

	return enabled, nil
}

func validateInput(c Check) error {
	switch c.ResourceID {
	case "":
		return InvalidCheck(c.Scope, c.ResourceID)
	case WildcardResource:
		return InvalidCheck(c.Scope, c.ResourceID)
	default:
		return nil
	}
}

func (m *Manager) mapError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, ErrDenied):
		return oops.C(oops.CodeForbidden)
	case errors.Is(err, ErrMissingGrants):
		return oops.E(oops.CodeUnexpected, err, "access grants missing from prepared context").Log(ctx, m.logger)
	case errors.Is(err, ErrInvalidCheck), errors.Is(err, ErrNoChecks):
		return oops.E(oops.CodeUnexpected, err, "invalid access check").Log(ctx, m.logger)
	default:
		return oops.E(oops.CodeUnexpected, err, "check access").Log(ctx, m.logger)
	}
}
