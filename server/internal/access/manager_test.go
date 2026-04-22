package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type stubFeatureChecker struct {
	enabled bool
	err     error
}

func (s stubFeatureChecker) IsFeatureEnabled(_ context.Context, _ string, _ productfeatures.Feature) (bool, error) {
	if s.err != nil {
		return false, s.err
	}

	return s.enabled, nil
}

func TestManagerRequire_requiresAuthContext(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)

	err := manager.Require(t.Context(), Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestManagerRequire_skipsWhenRBACFeatureDisabled(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: false}, workos.NewStubClient(), cache.NoopCache)

	err := manager.Require(enterpriseSessionCtx(t), Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestManagerRequire_mapsDeniedToForbidden(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	ctx := GrantsToContext(enterpriseSessionCtx(t), nil)

	err := manager.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestManagerRequire_mapsMissingGrantsToUnexpected(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)

	err := manager.Require(enterpriseSessionCtx(t), Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrMissingGrants)
}

func TestManagerRequire_returnsUnexpectedWhenFeatureCheckFails(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{err: errors.New("boom")}, workos.NewStubClient(), cache.NoopCache)

	err := manager.Require(enterpriseSessionCtx(t), Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
}

func TestResolveRoleSlug_cachesEmptyMembershipResult(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, "test@example.com", "Test User", "user_workos_test", "membership_test")

	membership := &countingMembershipFetcher{}
	manager := NewManager(testLogger(t), ti.conn, stubFeatureChecker{enabled: true}, membership, newMapCache())

	roleSlug, err := manager.resolveRoleSlug(ctx, authCtx.UserID, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, roleSlug)

	roleSlug, err = manager.resolveRoleSlug(ctx, authCtx.UserID, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, roleSlug)
	require.Equal(t, 1, membership.calls)
}

func TestManagerRequireAny_mapsDeniedToForbidden(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{{Scope: ScopeMCPConnect, Resource: "tool_a"}})

	err := manager.RequireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_b"},
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_c"},
	)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestManagerFilter_returnsAllowedSubset(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{{Scope: ScopeProjectRead, Resource: "proj_123"}})

	resourceIDs, err := manager.Filter(ctx, ScopeProjectRead, []string{"proj_123", "proj_456"})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_123"}, resourceIDs)
}

func TestManagerRequire_rejectsInvalidCheck(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{{Scope: ScopeProjectRead, Resource: WildcardResource}})

	err := manager.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: ""})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrInvalidCheck)
}

func TestManagerRequire_requiresChecks(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{{Scope: ScopeProjectRead, Resource: WildcardResource}})

	err := manager.Require(ctx)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.ErrorIs(t, err, ErrNoChecks)
}

func TestManagerRequire_skipsForAPIKeyAuth(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	sessionID := "session_123"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_123",
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "key_123",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})

	err := manager.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestManagerFilter_skipsForNonEnterpriseAccount(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	sessionID := "session_123"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_123",
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "pro",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})

	resourceIDs, err := manager.Filter(ctx, ScopeProjectRead, []string{"proj_123", "proj_456"})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_123", "proj_456"}, resourceIDs)
}

type countingMembershipFetcher struct {
	calls int
}

func (c *countingMembershipFetcher) GetOrgMembership(context.Context, string, string) (*workos.Member, error) {
	c.calls++
	return nil, nil
}

type mapCache struct {
	items map[string][]byte
}

func newMapCache() *mapCache {
	return &mapCache{items: map[string][]byte{}}
}

func (m *mapCache) Get(_ context.Context, key string, value any) error {
	item, ok := m.items[key]
	if !ok {
		return errors.New("cache miss")
	}
	if err := json.Unmarshal(item, value); err != nil {
		return fmt.Errorf("unmarshal cache item: %w", err)
	}
	return nil
}

func (m *mapCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	item, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal cache item: %w", err)
	}
	m.items[key] = item
	return nil
}

func (m *mapCache) Update(ctx context.Context, key string, value any) error {
	return m.Set(ctx, key, value, 0)
}

func (m *mapCache) Delete(_ context.Context, key string) error {
	delete(m.items, key)
	return nil
}

func (m *mapCache) ListAppend(context.Context, string, any, time.Duration) error {
	return errors.New("not implemented")
}

func (m *mapCache) ListRange(context.Context, string, int64, int64, any) error {
	return errors.New("not implemented")
}

func (m *mapCache) DeleteByPrefix(_ context.Context, prefix string) error {
	for key := range m.items {
		if strings.HasPrefix(key, prefix) {
			delete(m.items, key)
		}
	}
	return nil
}

func enterpriseSessionCtx(t *testing.T) context.Context {
	t.Helper()

	sessionID := "session_123"
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_123",
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})
}

// scopeOverrideCtx returns a context with the scope override header value set
// and an auth context for the given user/admin state.
func scopeOverrideCtx(t *testing.T, isAdmin bool, accountType string) context.Context {
	t.Helper()
	sessionID := "session_123"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_123",
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           accountType,
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               isAdmin,
	})
	return contextvalues.SetRBACScopeOverride(ctx, "project:read")
}

// TestCanUseOverride_devPlusAdmin verifies that an admin in a dev environment
// can activate the scope override.
func TestCanUseOverride_devPlusAdmin(t *testing.T) {
	t.Parallel()
	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: false}, workos.NewStubClient(), cache.NoopCache, ManagerOpts{DevMode: true})
	ctx := scopeOverrideCtx(t, true, "pro")

	enforce, err := manager.shouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

// TestCanUseOverride_devPlusNonAdmin verifies that any user in a dev environment
// can activate the scope override — the admin check is bypassed.
func TestCanUseOverride_devPlusNonAdmin(t *testing.T) {
	t.Parallel()
	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: false}, workos.NewStubClient(), cache.NoopCache, ManagerOpts{DevMode: true})
	ctx := scopeOverrideCtx(t, false, "pro")

	enforce, err := manager.shouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

// TestCanUseOverride_prodPlusAdmin verifies that a superadmin in production can
// activate the scope override.
func TestCanUseOverride_prodPlusAdmin(t *testing.T) {
	t.Parallel()
	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: false}, workos.NewStubClient(), cache.NoopCache)
	ctx := scopeOverrideCtx(t, true, "pro")

	enforce, err := manager.shouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
}

// TestCanUseOverride_prodPlusNonAdmin verifies that a non-admin in production
// cannot activate the scope override even when the header is present.
func TestCanUseOverride_prodPlusNonAdmin(t *testing.T) {
	t.Parallel()
	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: false}, workos.NewStubClient(), cache.NoopCache)
	ctx := scopeOverrideCtx(t, false, "pro")

	// Non-enterprise + no feature flag + not admin → RBAC not enforced (all allowed).
	enforce, err := manager.shouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce)
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.DiscardHandler)
}

type noopFeatureCacheWriter struct{}

func (noopFeatureCacheWriter) UpdateFeatureCache(_ context.Context, _ string, _ productfeatures.Feature, _ bool) {
}
