package projects_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/projects"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// whitelistStaffOrganizationID is the staff organization newWhitelistService
// configures in tests that need one.
const whitelistStaffOrganizationID = "org-staff"

func newWhitelistService(t *testing.T, ti *testInstance, staffOrg staff.Organization) *projects.Service {
	t.Helper()

	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	return projects.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.sessionManager, engine, audit.NewLogger(), nil, false, staffOrg)
}

func TestSetOrganizationWhitelist_RejectsWithoutStaffOrganization(t *testing.T) {
	t.Parallel()

	// newTestProjectsService configures no staff organization.
	ctx, ti := newTestProjectsService(t)

	err := ti.service.SetOrganizationWhitelist(ctx, &gen.SetOrganizationWhitelistPayload{OrganizationID: "org-customer", Whitelisted: true, ApikeyToken: nil})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestSetOrganizationWhitelist_RejectsNonStaffCaller(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	svc := newWhitelistService(t, ti, staff.NewOrganization(whitelistStaffOrganizationID))

	err := svc.SetOrganizationWhitelist(ctx, &gen.SetOrganizationWhitelistPayload{OrganizationID: "org-customer", Whitelisted: true, ApikeyToken: nil})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

// The caller acts in the staff organization and changes another organization.
// The permission checked is admin of the caller's own organization, because the
// engine refuses an org check about any organization but the caller's.
func TestSetOrganizationWhitelist_ChecksAdminOfTheCallersOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	target := authCtx.ActiveOrganizationID
	svc := newWhitelistService(t, ti, staff.NewOrganization(whitelistStaffOrganizationID))

	staffAuth := *authCtx
	staffAuth.ActiveOrganizationID = whitelistStaffOrganizationID
	staffCtx := contextvalues.SetAuthContext(ctx, &staffAuth)
	payload := &gen.SetOrganizationWhitelistPayload{OrganizationID: target, Whitelisted: true, ApikeyToken: nil}

	admin := authz.GrantsToContext(staffCtx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, whitelistStaffOrganizationID)})
	require.NoError(t, svc.SetOrganizationWhitelist(admin, payload))

	// Admin of the target organization is not the permission asked for.
	other := authz.GrantsToContext(staffCtx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, target)})
	err := svc.SetOrganizationWhitelist(other, payload)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
