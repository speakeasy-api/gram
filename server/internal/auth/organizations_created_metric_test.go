package auth_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics/orgmetricstest"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// offerDisablingWorkOSFetcher disables the partner offer while WorkOS creates
// the organization: after signup resolved the partner code, and before the
// provisioning transaction checks the offer again.
type offerDisablingWorkOSFetcher struct {
	*mockWorkOSFetcher

	// disableOffer runs before each WorkOS organization create.
	disableOffer func(context.Context)
}

func (f *offerDisablingWorkOSFetcher) CreateOrganization(ctx context.Context, name, gramOrgID string) (string, error) {
	f.disableOffer(ctx)
	return f.mockWorkOSFetcher.CreateOrganization(ctx, name, gramOrgID)
}

// organizationlessSessionContext stores a session with no active organization
// and returns a context authenticated as it.
func organizationlessSessionContext(t *testing.T, ctx context.Context, inst *testInstance, userInfo *MockUserInfo) (context.Context, string) {
	t.Helper()

	session := sessions.Session{
		SessionID:            uuid.NewString(),
		UserID:               userInfo.UserID,
		ActiveOrganizationID: "",
		WorkOSSessionID:      "",
		ImpersonatorEmail:    "",
	}
	require.NoError(t, inst.sessionManager.StoreSession(ctx, session))

	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: "",
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                &userInfo.Email,
		AccountType:          "test",
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	}), session.SessionID
}

func TestOrganizationsCreatedRegisterRecordsSignup(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	userInfo.Organizations = nil
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, userInfo)
	created, reader := orgmetricstest.New(t)
	inst.service.SetOrganizationsCreated(created)

	ctx, sessionID := organizationlessSessionContext(t, ctx, inst, userInfo)
	require.NoError(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: "Metric Signup Co", SessionToken: nil}))

	session, err := inst.sessionManager.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotEmpty(t, session.ActiveOrganizationID)
	reader.RequireOnly(t, orgmetrics.CreationSourceSignup, 1)
}

func TestOrganizationsCreatedCallbackSignupRecordsSignup(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	userInfo.Organizations = nil
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, userInfo)
	created, reader := orgmetricstest.New(t)
	inst.service.SetOrganizationsCreated(created)

	ctx, stateParam := inst.stateWithSignupIntent(ctx, t, "", "Acme Inc")
	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: new("mock_code"), State: &stateParam})
	require.NoError(t, err)
	require.NotContains(t, result.Location, "signin_error=")

	session, err := inst.sessionManager.GetSession(ctx, result.SessionToken)
	require.NoError(t, err)
	require.NotEmpty(t, session.ActiveOrganizationID)
	reader.RequireOnly(t, orgmetrics.CreationSourceSignup, 1)
}

// The partner path inserts the row with a create and then upserts it, and the
// organization counts once.
func TestOrganizationsCreatedPartnerSignupRecordsSignupOnce(t *testing.T) {
	t.Parallel()

	user := defaultMockUserInfo()
	user.Organizations = nil
	resolver := newSignupPromotionCodeResolver(t)
	resolver.On("ResolveGatewayPromotionCode", mock.Anything, "fixture").Return("promo_fixture", nil).Once()
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, user, partnerSignupConfiguration(t, resolver))
	seedPartnerOffer(t, ctx, inst)
	created, reader := orgmetricstest.New(t)
	inst.service.SetOrganizationsCreated(created)

	callback, err := inst.callbackWithNonce(ctx, t)
	require.NoError(t, err)
	require.Empty(t, reader.Counts(t), "a callback without a signup intent creates no organization")

	ctx, err = inst.sessionManager.Authenticate(ctx, callback.SessionToken)
	require.NoError(t, err)
	require.NoError(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: "Fixture Company", PartnerCode: conv.PtrEmpty(" FIXTURE ")}))

	session, err := inst.sessionManager.GetSession(ctx, callback.SessionToken)
	require.NoError(t, err)
	assertPartnerSignup(t, ctx, inst, session.ActiveOrganizationID)
	reader.RequireOnly(t, orgmetrics.CreationSourceSignup, 1)
}

func TestOrganizationsCreatedAssistantsRecordsAssistants(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	userInfo.Organizations = nil
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, userInfo)
	created, reader := orgmetricstest.New(t)
	inst.service.SetOrganizationsCreated(created)

	ctx, stateParam := inst.stateWithNonce(ctx, t, "/?disposition=assistants")
	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: new("mock_code"), State: &stateParam})
	require.NoError(t, err)
	require.NotContains(t, result.Location, "signin_error=")
	require.Contains(t, result.Location, "/assistants/new?disposition=assistants")

	reader.RequireOnly(t, orgmetrics.CreationSourceAssistants, 1)
}

// An organization row WorkOS sync already wrote is adopted by provisioning,
// and provisioning does not count it again.
func TestOrganizationsCreatedRegisterAdoptingExistingRowRecordsNothing(t *testing.T) {
	t.Parallel()

	const orgName = "Synced Org"
	userInfo := defaultMockUserInfo()
	userInfo.Organizations = nil
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, userInfo)
	created, reader := orgmetricstest.New(t)
	inst.service.SetOrganizationsCreated(created)

	// The mock WorkOS client names the organization it creates after orgName,
	// and the Speakeasy ID derives from that WorkOS ID.
	workosOrgID := "workos_org_" + orgName
	organizationID := orgid.FromWorkOSID(workosOrgID)
	require.NoError(t, inst.createTestOrganization(ctx, MockOrganizationEntry{
		ID:                 organizationID,
		Name:               orgName,
		Slug:               "synced-org-from-workos",
		WorkosID:           new(workosOrgID),
		UserWorkspaceSlugs: nil,
	}, ""))

	ctx, sessionID := organizationlessSessionContext(t, ctx, inst, userInfo)
	require.NoError(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: orgName, SessionToken: nil}))

	session, err := inst.sessionManager.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, organizationID, session.ActiveOrganizationID)
	require.Empty(t, reader.Counts(t))
}

// A partner signup whose transaction rolls back after inserting the row
// records nothing.
func TestOrganizationsCreatedRolledBackProvisioningRecordsNothing(t *testing.T) {
	t.Parallel()

	user := defaultMockUserInfo()
	user.Organizations = nil
	resolver := newSignupPromotionCodeResolver(t)
	resolver.On("ResolveGatewayPromotionCode", mock.Anything, "fixture").Return("promo_fixture", nil).Once()
	fetcher := &offerDisablingWorkOSFetcher{
		mockWorkOSFetcher: &mockWorkOSFetcher{
			members: map[string][]workos.Member{},
			orgs:    map[string]*workos.Organization{},
		},
		disableOffer: nil,
	}
	ctx, inst := newTestAuthServiceWithWorkOSClient(t, user, fetcher, partnerSignupConfiguration(t, resolver))
	require.NoError(t, inst.createTestUser(ctx, user))
	require.NoError(t, userRepo.New(inst.conn).OverwriteUserWorkosID(ctx, userRepo.OverwriteUserWorkosIDParams{
		ID:       user.UserID,
		WorkosID: conv.ToPGText(user.UserID),
	}))
	seedPartnerOffer(t, ctx, inst)
	fetcher.disableOffer = func(ctx context.Context) {
		changePartnerOffer(t, ctx, inst, "promo_fixture", "tailscale_partner_gateway", false)
	}
	created, reader := orgmetricstest.New(t)
	inst.service.SetOrganizationsCreated(created)

	ctx, _ = organizationlessSessionContext(t, ctx, inst, user)
	err := inst.service.Register(ctx, &gen.RegisterPayload{OrgName: "Fixture Company", PartnerCode: conv.PtrEmpty("fixture")})
	require.Error(t, err)

	// WorkOS created the organization, so provisioning reached the transaction
	// that inserts the row, and the rollback left no row behind.
	require.Len(t, fetcher.createdOrgs, 1)
	_, err = orgRepo.New(inst.conn).GetOrganizationMetadata(ctx, orgid.FromWorkOSID("workos_org_Fixture Company"))
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Empty(t, reader.Counts(t))
}

func TestOrganizationsCreatedPlatformMCPWorkspaceRecordsPlatformMCP(t *testing.T) {
	t.Parallel()

	userInfo := platformMCPWorkspaceUser()
	ctx, inst := newTestAuthServiceForOrganizationProvisioning(t, userInfo)
	created, reader := orgmetricstest.New(t)
	inst.service.SetOrganizationsCreated(created)

	_, _, err := inst.service.ProvisionPlatformMCPWorkspace(ctx, userInfo.UserID, "Acme Workspace", "Claude Code")
	require.NoError(t, err)
	reader.RequireOnly(t, orgmetrics.CreationSourcePlatformMCP, 1)

	// A refused second workspace inserts nothing.
	_, _, err = inst.service.ProvisionPlatformMCPWorkspace(ctx, userInfo.UserID, "Second Workspace", "Claude Code")
	require.Error(t, err)
	reader.RequireOnly(t, orgmetrics.CreationSourcePlatformMCP, 1)
}
