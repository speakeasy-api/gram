package organizations_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/svix/svix-webhooks/go/models"
)

const (
	testPortalToken = "portal-token-abc123"
	testPortalURL   = "https://portal.svix.com/login#key=abc123"
)

func seedSvixApp(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext) {
	t.Helper()
	ctx := t.Context()
	_, err := orgrepo.New(ti.conn).UpsertSvixAppID(ctx, orgrepo.UpsertSvixAppIDParams{
		ID:        authCtx.ActiveOrganizationID,
		SvixAppID: conv.ToPGText(testSvixAppID),
	})
	require.NoError(t, err)
}

func expectPortalSession(t *testing.T, ti *testInstance) {
	t.Helper()
	ti.svixSrv.On("CreateAppPortalSession", mock.Anything, testSvixAppID).
		Return(&models.AppPortalAccessOut{
			Token: testPortalToken,
			Url:   testPortalURL,
		}, nil).Once()
}

// TestService_CreatePortalSession verifies the happy path: org with webhooks enabled returns
// a valid portal URL and token.
func TestService_CreatePortalSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedSvixApp(t, ti, authCtx)
	expectPortalSession(t, ti)

	res, err := ti.service.CreatePortalSession(ctx, &gen.CreatePortalSessionPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, testPortalURL, res.URL)
	require.Equal(t, testPortalToken, res.Token)

	ti.svixSrv.AssertExpectations(t)
}

// TestService_CreatePortalSession_AdminGrantGetsViewBaseAccess verifies that a user with
// org:admin gets a portal session. Admins receive VIEW_BASE capabilities only (they manage
// webhooks through Gram's own UI, not the Svix portal).
func TestService_CreatePortalSession_AdminGrantGetsViewBaseAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	seedSvixApp(t, ti, authCtx)
	expectPortalSession(t, ti)

	res, err := ti.service.CreatePortalSession(ctx, &gen.CreatePortalSessionPayload{})
	require.NoError(t, err)
	require.Equal(t, testPortalURL, res.URL)
	require.Equal(t, testPortalToken, res.Token)

	ti.svixSrv.AssertExpectations(t)
}

// TestService_CreatePortalSession_ReadGrantGetsFullAccess verifies that a user with org:read
// but not org:admin gets a portal session with full Svix capabilities (they are a webhook
// consumer configuring their own endpoints).
func TestService_CreatePortalSession_ReadGrantGetsFullAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	seedSvixApp(t, ti, authCtx)
	expectPortalSession(t, ti)

	res, err := ti.service.CreatePortalSession(ctx, &gen.CreatePortalSessionPayload{})
	require.NoError(t, err)
	require.Equal(t, testPortalURL, res.URL)
	require.Equal(t, testPortalToken, res.Token)

	ti.svixSrv.AssertExpectations(t)
}

// TestService_CreatePortalSession_WebhooksNotEnabled verifies that a BadRequest error is
// returned when the org has a svix app but webhooks are disabled.
func TestService_CreatePortalSession_WebhooksNotEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Seed app ID but then explicitly disable webhooks.
	seedSvixApp(t, ti, authCtx)
	_, err := orgrepo.New(ti.conn).SetWebhooksEnabled(ctx, orgrepo.SetWebhooksEnabledParams{
		ID:      authCtx.ActiveOrganizationID,
		Enabled: pgtype.Bool{Bool: false, Valid: true},
	})
	require.NoError(t, err)

	_, err = ti.service.CreatePortalSession(ctx, &gen.CreatePortalSessionPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

// TestService_CreatePortalSession_NoSvixApp verifies that a BadRequest error is returned
// when no svix app has been set up for the org.
func TestService_CreatePortalSession_NoSvixApp(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	_, err := ti.service.CreatePortalSession(ctx, &gen.CreatePortalSessionPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}
