package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func goaCtx(ctx context.Context, service, method string) context.Context {
	ctx = context.WithValue(ctx, goa.ServiceKey, service)
	return context.WithValue(ctx, goa.MethodKey, method)
}

func setWhitelisted(t *testing.T, ctx context.Context, instance *testInstance, orgID string, whitelisted bool) {
	t.Helper()
	require.NoError(t, testrepo.New(instance.conn).SetOrganizationServingStateFixture(ctx, testrepo.SetOrganizationServingStateFixtureParams{
		ID: orgID, Whitelisted: whitelisted, Disabled: false,
	}))
}

func TestAuthorizeSessionRefusesNonWhitelistedOrganization(t *testing.T) {
	t.Parallel()

	ctx, instance, _ := newProjectAccessTest(t)
	userInfo := defaultMockUserInfo()
	orgID := userInfo.Organizations[0].ID
	session := sessions.Session{
		SessionID:            "not-whitelisted-session",
		ActiveOrganizationID: orgID,
		UserID:               userInfo.UserID,
		WorkOSSessionID:      "workos-not-whitelisted-session",
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))
	setWhitelisted(t, ctx, instance, orgID, false)

	_, err := instance.authorizer.Authorize(goaCtx(t.Context(), "toolsets", "listToolsets"), session.SessionID, sessionScheme)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	// Billing stays reachable so a user whose trial expired can pay.
	_, err = instance.authorizer.Authorize(goaCtx(t.Context(), "usage", "createStripeCheckout"), session.SessionID, sessionScheme)
	require.NoError(t, err)
	_, err = instance.authorizer.Authorize(goaCtx(t.Context(), "access", "listGrants"), session.SessionID, sessionScheme)
	require.NoError(t, err)
	_, err = instance.authorizer.Authorize(goaCtx(t.Context(), "access", "listRoles"), session.SessionID, sessionScheme)
	require.ErrorAs(t, err, &oopsErr)

	// Access returns on the next request once staff whitelist the organization.
	setWhitelisted(t, ctx, instance, orgID, true)
	_, err = instance.authorizer.Authorize(goaCtx(t.Context(), "toolsets", "listToolsets"), session.SessionID, sessionScheme)
	require.NoError(t, err)
}

func TestAuthorizeSessionAllowsSupportSessionForNonWhitelistedOrganization(t *testing.T) {
	t.Parallel()

	userInfo := adminMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	require.NoError(t, instance.createTestOrganization(ctx, userInfo.Organizations[0], userInfo.UserID))

	customerOrg := MockOrganizationEntry{ID: "customer-org-not-whitelisted", Name: "Customer Corp", Slug: "customer-not-whitelisted"}
	require.NoError(t, instance.createTestOrganization(ctx, customerOrg, ""))
	setWhitelisted(t, ctx, instance, customerOrg.ID, false)

	session := sessions.Session{
		SessionID:             "support-not-whitelisted",
		UserID:                userInfo.UserID,
		ActiveOrganizationID:  customerOrg.ID,
		WorkOSSessionID:       "workos-support-not-whitelisted",
		SupportOrganizationID: customerOrg.ID,
		SupportExpiresAt:      time.Now().Add(time.Hour),
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	_, err := instance.authorizer.Authorize(goaCtx(t.Context(), "toolsets", "listToolsets"), session.SessionID, sessionScheme)
	require.NoError(t, err)
}

func TestAuthorizeAPIKeyIgnoresWhitelist(t *testing.T) {
	t.Parallel()

	ctx, instance, _ := newProjectAccessTest(t)
	key := createTestAPIKey(t, ctx, instance, nil)
	setWhitelisted(t, ctx, instance, defaultMockUserInfo().Organizations[0].ID, false)

	_, err := instance.authorizer.Authorize(goaCtx(t.Context(), "toolsets", "listToolsets"), key, apiKeyScheme)
	require.NoError(t, err)
}
