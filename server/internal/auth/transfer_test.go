package auth_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// transferSession stores a session for userInfo's first organization and
// returns a context authenticated with it on the server host.
func transferSession(t *testing.T, ctx context.Context, instance *testInstance, userInfo *MockUserInfo, impersonator string) (context.Context, sessions.Session) {
	t.Helper()

	session := sessions.Session{
		SessionID:             "source-" + t.Name(),
		UserID:                userInfo.UserID,
		ActiveOrganizationID:  userInfo.Organizations[0].ID,
		WorkOSSessionID:       "workos-session-id",
		ImpersonatorEmail:     impersonator,
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		AccountType:          "test",
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                &userInfo.Email,
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	})
	return requestorigin.WithContext(ctx, originAt(requestorigin.SurfacePlatform, testServerURL.String())), session
}

// transferOutCode runs TransferOut to the extra platform host and returns the
// transfer code from the redirect.
func transferOutCode(t *testing.T, ctx context.Context, instance *testInstance) string {
	t.Helper()

	result, err := transferOut(ctx, instance)
	require.NoError(t, err)

	location, err := url.Parse(result.Location)
	require.NoError(t, err)
	require.Equal(t, testExtraPlatformHost, location.Host)
	require.Equal(t, "/rpc/auth.transferIn", location.Path)
	code := location.Query().Get("token")
	require.NotEmpty(t, code)
	return code
}

func transferOut(ctx context.Context, instance *testInstance) (*gen.TransferOutResult, error) {
	return instance.service.TransferOut(ctx, &gen.TransferOutPayload{ //nolint:wrapcheck // test helper returns the service error as is
		TargetHost:   testExtraPlatformHost,
		Redirect:     nil,
		SessionToken: nil,
	})
}

func transferIn(ctx context.Context, instance *testInstance, host, code string) (*gen.TransferInResult, error) {
	ctx = requestorigin.WithContext(ctx, originAt(requestorigin.SurfacePlatform, host))
	return instance.service.TransferIn(ctx, &gen.TransferInPayload{Token: code, Redirect: nil}) //nolint:wrapcheck // test helper returns the service error as is
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// newTransferInstance seeds a user and their organization. With member false
// the user is not a member of the organization.
func newTransferInstance(t *testing.T, member bool) (context.Context, *testInstance, *MockUserInfo) {
	t.Helper()
	userInfo := defaultMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	memberID := ""
	if member {
		memberID = userInfo.UserID
	}
	require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], memberID))
	return ctx, instance, userInfo
}

func TestService_Transfer_RoundTrip(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, true)
	ctx, source := transferSession(t, ctx, instance, userInfo, "")

	code := transferOutCode(t, ctx, instance)
	require.NotContains(t, code, source.SessionID)

	result, err := transferIn(ctx, instance, "https://"+testExtraPlatformHost, code)
	require.NoError(t, err)
	require.Equal(t, "/", result.Location)
	require.NotEqual(t, source.SessionID, result.SessionToken)

	minted, err := instance.sessionManager.GetSession(ctx, result.SessionToken)
	require.NoError(t, err)
	require.Equal(t, source.UserID, minted.UserID)
	require.Equal(t, source.ActiveOrganizationID, minted.ActiveOrganizationID)
	require.Equal(t, source.WorkOSSessionID, minted.WorkOSSessionID)
	require.Empty(t, minted.ImpersonatorEmail)

	_, err = transferIn(ctx, instance, "https://"+testExtraPlatformHost, code)
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestService_TransferOut_RejectsImpersonation(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, true)
	ctx, _ = transferSession(t, ctx, instance, userInfo, "support@example.com")

	_, err := transferOut(ctx, instance)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_TransferIn_WrongHostDoesNotConsume(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, true)
	ctx, _ = transferSession(t, ctx, instance, userInfo, "")
	code := transferOutCode(t, ctx, instance)

	_, err := transferIn(ctx, instance, testServerURL.String(), code)
	requireOopsCode(t, err, oops.CodeUnauthorized)

	_, err = transferIn(ctx, instance, "https://"+testExtraPlatformHost, code)
	require.NoError(t, err)
}

func TestService_TransferIn_NonMemberDoesNotConsume(t *testing.T) {
	t.Parallel()

	ctx, instance, userInfo := newTransferInstance(t, false)
	ctx, _ = transferSession(t, ctx, instance, userInfo, "")
	code := transferOutCode(t, ctx, instance)

	_, err := transferIn(ctx, instance, "https://"+testExtraPlatformHost, code)
	requireOopsCode(t, err, oops.CodeForbidden)

	require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID))
	_, err = transferIn(ctx, instance, "https://"+testExtraPlatformHost, code)
	require.NoError(t, err)
}
