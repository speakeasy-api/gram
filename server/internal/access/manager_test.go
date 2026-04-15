package access

import (
	"context"
	"errors"
	"log/slog"
	"testing"

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

	err := manager.Require(t.Context(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestManagerRequire_skipsWhenRBACFeatureDisabled(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: false}, workos.NewStubClient(), cache.NoopCache)

	err := manager.Require(enterpriseSessionCtx(t), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestManagerRequire_mapsDeniedToForbidden(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	ctx := GrantsToContext(enterpriseSessionCtx(t), &Grants{rows: nil})

	err := manager.Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestManagerRequire_mapsMissingGrantsToUnexpected(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)

	err := manager.Require(enterpriseSessionCtx(t), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.ErrorIs(t, err, ErrMissingGrants)
}

func TestManagerRequire_returnsUnexpectedWhenFeatureCheckFails(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{err: errors.New("boom")}, workos.NewStubClient(), cache.NoopCache)

	err := manager.Require(enterpriseSessionCtx(t), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	requireOopsCode(t, err, oops.CodeUnexpected)
}

func TestManagerRequireAny_mapsDeniedToForbidden(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	ctx := GrantsToContext(enterpriseSessionCtx(t), &Grants{rows: []Grant{{Scope: ScopeMCPConnect, Resource: "tool_a"}}})

	err := manager.RequireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_b"},
		Check{Scope: ScopeMCPConnect, ResourceID: "tool_c"},
	)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestManagerFilter_returnsAllowedSubset(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	ctx := GrantsToContext(enterpriseSessionCtx(t), &Grants{rows: []Grant{{Scope: ScopeBuildRead, Resource: "proj_123"}}})

	resourceIDs, err := manager.Filter(ctx, ScopeBuildRead, []string{"proj_123", "proj_456"})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_123"}, resourceIDs)
}

func TestManagerRequire_rejectsInvalidCheck(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	ctx := GrantsToContext(enterpriseSessionCtx(t), &Grants{rows: []Grant{{Scope: ScopeBuildRead, Resource: WildcardResource}}})

	err := manager.Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: ""})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.ErrorIs(t, err, ErrInvalidCheck)
}

func TestManagerRequire_requiresChecks(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)
	ctx := GrantsToContext(enterpriseSessionCtx(t), &Grants{rows: []Grant{{Scope: ScopeBuildRead, Resource: WildcardResource}}})

	err := manager.Require(ctx)
	requireOopsCode(t, err, oops.CodeUnexpected)
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

	err := manager.Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
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

	resourceIDs, err := manager.Filter(ctx, ScopeBuildRead, []string{"proj_123", "proj_456"})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_123", "proj_456"}, resourceIDs)
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

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, code, shareableErr.Code)
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.DiscardHandler)
}

type noopFeatureCacheWriter struct{}

func (noopFeatureCacheWriter) UpdateFeatureCache(_ context.Context, _ string, _ productfeatures.Feature, _ bool) {
}
