package access

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

func TestManagerPrepareContext_requiresAuthContext(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true})

	_, err := manager.PrepareContext(t.Context())
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestManagerRequire_skipsWhenRBACFeatureDisabled(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: false})

	err := manager.Require(enterpriseSessionCtx(t), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestManagerRequire_mapsDeniedToForbidden(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true})
	ctx := GrantsToContext(enterpriseSessionCtx(t), &Grants{rows: nil})

	err := manager.Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestManagerMapError_mapsMissingGrantsToUnexpected(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{enabled: true})
	ctx := t.Context()

	err := manager.mapError(ctx, ErrMissingGrants)
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.ErrorIs(t, err, ErrMissingGrants)
}

func TestManagerRequire_loadsGrantsIntoContext(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newInternalTestService(t)
	organizationID := "org_manager_loads"
	userID := "user_manager_loads"
	sessionID := "session_manager_loads"

	seedInternalOrganization(t, ctx, conn, organizationID)
	seedInternalGrant(t, ctx, conn, organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, userID), string(ScopeBuildRead), "proj_123")

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  organizationID,
		UserID:                userID,
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

	manager := NewManager(svc.logger, conn, stubFeatureChecker{enabled: true})

	err := manager.Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestManagerRequire_returnsUnexpectedWhenFeatureCheckFails(t *testing.T) {
	t.Parallel()

	manager := NewManager(testLogger(t), nil, stubFeatureChecker{err: errors.New("boom")})

	err := manager.Require(enterpriseSessionCtx(t), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	requireOopsCode(t, err, oops.CodeUnexpected)
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
	return testenv.NewLogger(t)
}
