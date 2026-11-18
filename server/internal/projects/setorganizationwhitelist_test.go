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
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/projects"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func newWhitelistService(t *testing.T, ti *testInstance, staffOrg staff.Organization) *projects.Service {
	t.Helper()

	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	return projects.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.sessionManager, engine, audit.NewLogger(), nil, false, staffOrg)
}

func TestProjectsService_SetOrganizationWhitelist_RejectsWithoutStaffOrganization(t *testing.T) {
	t.Parallel()

	// newTestProjectsService configures no staff organization.
	ctx, ti := newTestProjectsService(t)

	err := ti.service.SetOrganizationWhitelist(ctx, &gen.SetOrganizationWhitelistPayload{
		OrganizationID: "org-customer",
		Whitelisted:    true,
		ApikeyToken:    nil,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestProjectsService_SetOrganizationWhitelist_RejectsNonStaffCaller(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	svc := newWhitelistService(t, ti, staff.NewOrganization("org-other-staff"))

	err := svc.SetOrganizationWhitelist(ctx, &gen.SetOrganizationWhitelistPayload{
		OrganizationID: "org-customer",
		Whitelisted:    true,
		ApikeyToken:    nil,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestProjectsService_SetOrganizationWhitelist_AllowsStaffCaller(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	ctx = withAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)})
	svc := newWhitelistService(t, ti, staff.NewOrganization(orgID))

	err := svc.SetOrganizationWhitelist(ctx, &gen.SetOrganizationWhitelistPayload{
		OrganizationID: orgID,
		Whitelisted:    true,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)

	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	require.True(t, org.Whitelisted)
}
