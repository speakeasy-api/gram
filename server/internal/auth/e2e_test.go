package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/usermanagement"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/users"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// --- mock WorkOS membership fetcher ---

type mockWorkOSFetcher struct {
	members map[string][]workos.Member      // keyed by WorkOS user ID
	orgs    map[string]*workos.Organization // keyed by WorkOS org ID
}

func (m *mockWorkOSFetcher) ListUserMemberships(_ context.Context, userID string) ([]workos.Member, error) {
	return m.members[userID], nil
}

func (m *mockWorkOSFetcher) GetOrganization(_ context.Context, orgID string) (*workos.Organization, error) {
	if org, ok := m.orgs[orgID]; ok {
		return org, nil
	}
	return nil, &workos.APIError{StatusCode: 404, Body: "not found"}
}

func (m *mockWorkOSFetcher) EnsureUserExternalID(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockWorkOSFetcher) EnsureOrgExternalID(_ context.Context, _, _ string) error {
	return nil
}

// --- test setup that wires a WorkOSMembershipFetcher into the session manager ---

type e2eInstance struct {
	testInstance
	fetcher *mockWorkOSFetcher
}

func newE2EAuthService(t *testing.T, userInfo *MockUserInfo, fetcher *mockWorkOSFetcher) (context.Context, *e2eInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "authtest")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	mockServer := createMockWorkOSServer(userInfo)
	t.Cleanup(mockServer.Close)

	umClient := usermanagement.NewClient("test-api-key")
	umClient.Endpoint = mockServer.URL
	umClient.HTTPClient = mockServer.Client()

	pylonClient, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	posthogClient := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")
	billingClient := billing.NewStubClient(logger, tracerProvider)

	var wf sessions.WorkOSClient
	if fetcher != nil {
		wf = fetcher
	}

	sessionManager := sessions.NewManager(
		logger, tracerProvider, conn, redisClient, cache.Suffix("gram-e2e"),
		mockServer.URL, "test-client-id", umClient, wf,
		pylonClient, posthogClient, billingClient,
	)

	authConfigs := auth.AuthConfigurations{
		IDPBaseURL:        mockServer.URL,
		GramServerURL:     "http://localhost:8080",
		SignInRedirectURL: "http://localhost:3000/dashboard",
		Environment:       "test",
	}

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	svc := auth.NewService(logger, tracerProvider, conn, sessionManager, authConfigs, authzEngine, billingClient, noopCancelScheduler{}, posthogClient)

	ti := newTestAuthServiceResult(t, svc, conn, sessionManager, mockServer, authConfigs)
	return ctx, &e2eInstance{testInstance: *ti, fetcher: fetcher}
}

// setUserWorkosID stamps the WorkOS user ID on an existing Gram user so the
// fallback path can fire.
func (e *e2eInstance) setUserWorkosID(ctx context.Context, t *testing.T, gramUserID, workosUserID string) {
	t.Helper()
	q := usersRepo.New(e.conn)
	err := q.SetUserWorkosID(ctx, usersRepo.SetUserWorkosIDParams{
		ID:       gramUserID,
		WorkosID: pgtype.Text{String: workosUserID, Valid: true},
	})
	require.NoError(t, err)
}

// --- E2E tests ---

// TestE2E_Callback_NewUserWithWorkOSOrgMemberships exercises the production
// flow where a brand-new user logs in for the first time. The WorkOS sync job
// has not run yet, so the local DB has no org metadata or relationships. The
// session manager falls back to the WorkOS API, upserts the org and
// relationship, and the callback proceeds normally.
func TestE2E_Callback_NewUserWithWorkOSOrgMemberships(t *testing.T) {
	t.Parallel()

	const (
		workosUserID = "user_01WORKOS_NEW"
		workosOrgID  = "org_01WORKOS_ACME"
		orgName      = "Acme Corp"
	)

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			workosUserID: {
				{ID: "om_01ABC", UserID: workosUserID, OrganizationID: workosOrgID, Organization: orgName, RoleSlug: "member"},
			},
		},
		orgs: map[string]*workos.Organization{
			workosOrgID: {ID: workosOrgID, Name: orgName},
		},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "alice@acme.com",
		Organizations: []MockOrganizationEntry{}, // DB starts empty
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	// Callback fires code exchange → UpsertUserFromIDP → BuildUserInfoFromDB.
	// No pre-seeding — the fallback must create the org and relationship.
	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotContains(t, result.Location, "signin_error=", "callback should succeed without error")
	require.NotEmpty(t, result.SessionToken)

	// The org ID is now derived via UUIDv5 (no pre-seeded org, no external_id).
	expectedOrgID := orgid.FromWorkOSID(workosOrgID)

	// Verify the session is functional and points at the correct org.
	ctx, err = inst.sessionManager.Authenticate(ctx, result.SessionToken)
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	assert.Equal(t, expectedOrgID, authCtx.ActiveOrganizationID)

	// Verify the org was persisted in DB with the derived ID.
	orgMeta, err := orgRepo.New(inst.conn).GetOrganizationMetadata(ctx, expectedOrgID)
	require.NoError(t, err)
	assert.Equal(t, orgName, orgMeta.Name)
	assert.Equal(t, workosOrgID, orgMeta.WorkosID.String)
}

// TestE2E_Callback_NewUserJoiningExistingOrg verifies that when a new user
// joins an org that already has metadata in the DB (from another user), the
// fallback creates the relationship without corrupting the existing org row.
func TestE2E_Callback_NewUserJoiningExistingOrg(t *testing.T) {
	t.Parallel()

	const (
		workosUserID = "user_01WORKOS_BOB"
		workosOrgID  = "org_01EXISTING"
		orgName      = "Existing Corp"
	)

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			workosUserID: {
				{ID: "om_02DEF", UserID: workosUserID, OrganizationID: workosOrgID, Organization: orgName, RoleSlug: "admin"},
			},
		},
		orgs: map[string]*workos.Organization{
			workosOrgID: {ID: workosOrgID, Name: orgName},
		},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "bob@existing.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	// Pre-seed the org (as if another user already exists in this org).
	orgQueries := orgRepo.New(inst.conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          workosOrgID,
		Name:        orgName,
		Slug:        "existing-corp",
		WorkosID:    pgtype.Text{String: workosOrgID, Valid: true},
		Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)

	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotContains(t, result.Location, "signin_error=")
	require.NotEmpty(t, result.SessionToken)

	ctx, err = inst.sessionManager.Authenticate(ctx, result.SessionToken)
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	assert.Equal(t, workosOrgID, authCtx.ActiveOrganizationID)

	// Verify the org's whitelisted flag was NOT overwritten by the fallback.
	orgMeta, err := orgQueries.GetOrganizationMetadata(ctx, workosOrgID)
	require.NoError(t, err)
	assert.True(t, orgMeta.Whitelisted, "existing org's whitelisted flag must be preserved")
}

// TestE2E_Callback_NewUserNoWorkOSOrgs verifies that when a new user has no
// orgs in WorkOS at all, the zero-org flow (onboarding redirect) still works.
func TestE2E_Callback_NewUserNoWorkOSOrgs(t *testing.T) {
	t.Parallel()

	const workosUserID = "user_01WORKOS_EMPTY"

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{}, // no memberships
		orgs:    map[string]*workos.Organization{},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "empty@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotNil(t, result)
	// Zero-org users get a session but no active org.
	require.NotEmpty(t, result.SessionToken)
	require.NotContains(t, result.Location, "signin_error=")
	assert.Equal(t, inst.authConfigs.SignInRedirectURL, result.Location)
}

// TestE2E_Callback_NewUserNoWorkOSOrgs_AssistantsDisposition verifies that a
// new user with zero orgs and the "assistants" disposition gets auto-provisioned.
func TestE2E_Callback_NewUserNoWorkOSOrgs_AssistantsDisposition(t *testing.T) {
	t.Parallel()

	const workosUserID = "user_01WORKOS_ASSIST"

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{},
		orgs:    map[string]*workos.Organization{},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "assist@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	stateJSON, err := json.Marshal(map[string]string{"final_destination_url": "/?disposition=assistants"})
	require.NoError(t, err)
	stateParam := base64.RawURLEncoding.EncodeToString(stateJSON)

	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code", State: &stateParam})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotContains(t, result.Location, "signin_error=")
	assert.Contains(t, result.Location, "assistants")
	require.NotEmpty(t, result.SessionToken)
}

// TestE2E_Callback_ExistingUserWithDBOrgs verifies the happy path: user
// already has orgs in the DB, so the WorkOS fallback is skipped entirely.
func TestE2E_Callback_ExistingUserWithDBOrgs(t *testing.T) {
	t.Parallel()

	const workosUserID = "user_01WORKOS_EXISTING"

	// Fetcher has memberships, but they should NOT be consulted because the DB
	// already has org data.
	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			workosUserID: {{ID: "om_99", UserID: workosUserID, OrganizationID: "org_SHOULD_NOT_APPEAR", Organization: "Ghost", RoleSlug: "admin"}},
		},
		orgs: map[string]*workos.Organization{
			"org_SHOULD_NOT_APPEAR": {ID: "org_SHOULD_NOT_APPEAR", Name: "Ghost"},
		},
	}

	existingOrgWorkosID := "org_01DB_EXISTING"
	orgEntry := MockOrganizationEntry{
		ID:                 "org_01DB_EXISTING",
		Name:               "DB Corp",
		Slug:               "db-corp",
		WorkosID:           &existingOrgWorkosID,
		UserWorkspaceSlugs: []string{"db-corp"},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "exists@dbcorp.com",
		Organizations: []MockOrganizationEntry{orgEntry},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	require.NoError(t, inst.createTestUser(ctx, userInfo))
	require.NoError(t, inst.createTestOrganization(ctx, orgEntry, userInfo.UserID))
	inst.setUserWorkosID(ctx, t, userInfo.UserID, workosUserID)

	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotContains(t, result.Location, "signin_error=")
	require.NotEmpty(t, result.SessionToken)

	ctx, err = inst.sessionManager.Authenticate(ctx, result.SessionToken)
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	assert.Equal(t, orgEntry.ID, authCtx.ActiveOrganizationID, "should use DB org, not WorkOS ghost org")
}

// TestE2E_Callback_RejoinedOrg verifies that when a user left an org (soft-deleted
// relationship) and then rejoined in WorkOS, the fallback clears deleted_at.
func TestE2E_Callback_RejoinedOrg(t *testing.T) {
	t.Parallel()

	const (
		workosUserID = "user_01WORKOS_REJOIN"
		workosOrgID  = "org_01REJOIN"
		orgName      = "Rejoin Corp"
	)

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			workosUserID: {
				{ID: "om_03GHI", UserID: workosUserID, OrganizationID: workosOrgID, Organization: orgName, RoleSlug: "member"},
			},
		},
		orgs: map[string]*workos.Organization{
			workosOrgID: {ID: workosOrgID, Name: orgName},
		},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "rejoin@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	// Pre-seed the org.
	orgQueries := orgRepo.New(inst.conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:       workosOrgID,
		Name:     orgName,
		Slug:     "rejoin-corp",
		WorkosID: pgtype.Text{String: workosOrgID, Valid: true},
	})
	require.NoError(t, err)

	// Create the user first via callback so UpsertUserFromIDP runs.
	// Then soft-delete the relationship to simulate "user left".
	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotContains(t, result.Location, "signin_error=")

	// The Gram user ID is a UUIDv5 derived from the WorkOS user ID.
	gramUserID := users.UserIDFromWorkOSID(workosUserID)

	// Soft-delete the relationship via the SQLc method.
	err = orgRepo.New(inst.conn).DeleteOrganizationUserRelationship(ctx, orgRepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: workosOrgID,
		UserID:         gramUserID,
	})
	require.NoError(t, err)

	// Invalidate cache so the next login re-reads from DB.
	require.NoError(t, inst.sessionManager.InvalidateUserInfoCache(ctx, gramUserID))

	// Second login: relationship is soft-deleted, so ListOrganizationsForUser
	// returns 0 rows. The fallback should fire and clear deleted_at.
	result2, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotContains(t, result2.Location, "signin_error=")
	require.NotEmpty(t, result2.SessionToken)

	ctx, err = inst.sessionManager.Authenticate(ctx, result2.SessionToken)
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	assert.Equal(t, workosOrgID, authCtx.ActiveOrganizationID, "should be active after rejoin")
}

// TestE2E_Callback_MultipleOrgs verifies that when WorkOS returns multiple
// org memberships, all are synced and the first is selected as active.
func TestE2E_Callback_MultipleOrgs(t *testing.T) {
	t.Parallel()

	const (
		workosUserID = "user_01WORKOS_MULTI"
		orgID1       = "org_01MULTI_A"
		orgID2       = "org_01MULTI_B"
	)

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			workosUserID: {
				{ID: "om_A", UserID: workosUserID, OrganizationID: orgID1, Organization: "Alpha Inc", RoleSlug: "admin"},
				{ID: "om_B", UserID: workosUserID, OrganizationID: orgID2, Organization: "Beta LLC", RoleSlug: "member"},
			},
		},
		orgs: map[string]*workos.Organization{
			orgID1: {ID: orgID1, Name: "Alpha Inc"},
			orgID2: {ID: orgID2, Name: "Beta LLC"},
		},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "multi@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotContains(t, result.Location, "signin_error=")
	require.NotEmpty(t, result.SessionToken)

	// Org IDs are derived via UUIDv5 (no pre-seeded orgs, no external_id).
	expectedOrgID1 := orgid.FromWorkOSID(orgID1)
	expectedOrgID2 := orgid.FromWorkOSID(orgID2)

	// Verify both orgs exist in DB with derived IDs.
	orgQueries := orgRepo.New(inst.conn)
	org1, err := orgQueries.GetOrganizationMetadata(ctx, expectedOrgID1)
	require.NoError(t, err)
	assert.Equal(t, "Alpha Inc", org1.Name)

	org2, err := orgQueries.GetOrganizationMetadata(ctx, expectedOrgID2)
	require.NoError(t, err)
	assert.Equal(t, "Beta LLC", org2.Name)
}

// TestE2E_Callback_ThenInfo exercises the full login→info flow.
// After callback creates the session, calling Info should return the user's
// orgs (populated via the WorkOS fallback) with a default project.
func TestE2E_Callback_ThenInfo(t *testing.T) {
	t.Parallel()

	const (
		workosUserID = "user_01WORKOS_INFO"
		workosOrgID  = "org_01INFO_CORP"
		orgName      = "Info Corp"
	)

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			workosUserID: {
				{ID: "om_INFO", UserID: workosUserID, OrganizationID: workosOrgID, Organization: orgName, RoleSlug: "admin"},
			},
		},
		orgs: map[string]*workos.Organization{
			workosOrgID: {ID: workosOrgID, Name: orgName},
		},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "info@infocorp.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	// Step 1: Callback
	callbackResult, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotContains(t, callbackResult.Location, "signin_error=")

	// Step 2: Authenticate with the session token
	ctx, err = inst.sessionManager.Authenticate(ctx, callbackResult.SessionToken)
	require.NoError(t, err)

	// Step 3: Call Info
	infoResult, err := inst.service.Info(ctx, &gen.InfoPayload{})
	require.NoError(t, err)
	require.NotNil(t, infoResult)

	assert.Equal(t, users.UserIDFromWorkOSID(workosUserID), infoResult.UserID)
	assert.Equal(t, "info@infocorp.com", infoResult.UserEmail)
	assert.Equal(t, orgid.FromWorkOSID(workosOrgID), infoResult.ActiveOrganizationID)
	require.Len(t, infoResult.Organizations, 1)
	assert.Equal(t, orgName, infoResult.Organizations[0].Name)
	assert.NotEmpty(t, infoResult.Organizations[0].Projects, "default project should be auto-created")
}

// TestE2E_Callback_ThenSwitchScopes exercises: login → info → switch to
// second org. The second org was also created via the WorkOS fallback.
func TestE2E_Callback_ThenSwitchScopes(t *testing.T) {
	t.Parallel()

	const (
		workosUserID = "user_01WORKOS_SWITCH"
		orgID1       = "org_01SWITCH_A"
		orgID2       = "org_01SWITCH_B"
	)

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			workosUserID: {
				{ID: "om_SW_A", UserID: workosUserID, OrganizationID: orgID1, Organization: "Switch Alpha", RoleSlug: "admin"},
				{ID: "om_SW_B", UserID: workosUserID, OrganizationID: orgID2, Organization: "Switch Beta", RoleSlug: "member"},
			},
		},
		orgs: map[string]*workos.Organization{
			orgID1: {ID: orgID1, Name: "Switch Alpha"},
			orgID2: {ID: orgID2, Name: "Switch Beta"},
		},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "switch@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	// Step 1: Callback (creates session with orgID1 as active)
	callbackResult, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotContains(t, callbackResult.Location, "signin_error=")

	// Org IDs are derived via UUIDv5 (no pre-seeded orgs).
	gramOrgID1 := orgid.FromWorkOSID(orgID1)
	gramOrgID2 := orgid.FromWorkOSID(orgID2)

	ctx, err = inst.sessionManager.Authenticate(ctx, callbackResult.SessionToken)
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	assert.Equal(t, gramOrgID1, authCtx.ActiveOrganizationID)

	// Step 2: SwitchScopes to orgID2
	switchResult, err := inst.service.SwitchScopes(ctx, &gen.SwitchScopesPayload{OrganizationID: &gramOrgID2})
	require.NoError(t, err)
	require.NotNil(t, switchResult)

	ctx, err = inst.sessionManager.Authenticate(ctx, switchResult.SessionToken)
	require.NoError(t, err)
	authCtx, ok = contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	assert.Equal(t, gramOrgID2, authCtx.ActiveOrganizationID, "should have switched to second org")
}

// TestE2E_Callback_NoWorkOSClient verifies that when no WorkOS client is
// configured (nil), the fallback is silently skipped and the zero-org flow
// proceeds normally.
func TestE2E_Callback_NoWorkOSClient(t *testing.T) {
	t.Parallel()

	userInfo := &MockUserInfo{
		UserID:        "user_01_NIL_CLIENT",
		Email:         "nilclient@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, nil) // nil fetcher

	result, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotContains(t, result.Location, "signin_error=")
	require.NotEmpty(t, result.SessionToken)
	assert.Equal(t, inst.authConfigs.SignInRedirectURL, result.Location)
}

// TestE2E_Login_BuildsAuthorizationURL verifies that Login returns a valid
// authorization URL pointing at the mock IDP.
func TestE2E_Login(t *testing.T) {
	t.Parallel()

	userInfo := &MockUserInfo{
		UserID:        "user_01LOGIN",
		Email:         "login@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	_, inst := newE2EAuthService(t, userInfo, nil)

	result, err := inst.service.Login(t.Context(), &gen.LoginPayload{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Location, "/authorize")
	assert.Contains(t, result.Location, "client_id=test-client-id")
	assert.Contains(t, result.Location, "response_type=code")
}

// TestE2E_Login_WithRedirect verifies that the redirect parameter is encoded
// in the state and the authorization URL is still valid.
func TestE2E_Login_WithRedirect(t *testing.T) {
	t.Parallel()

	userInfo := &MockUserInfo{
		UserID:        "user_01LOGIN_REDIR",
		Email:         "redir@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	_, inst := newE2EAuthService(t, userInfo, nil)

	redirect := "/org/my-org/dashboard"
	result, err := inst.service.Login(t.Context(), &gen.LoginPayload{Redirect: &redirect})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Location, "/authorize")
	assert.Contains(t, result.Location, "state=")
}

// TestE2E_Logout verifies that after logging out the session is invalidated
// and cannot be used to authenticate.
func TestE2E_Logout(t *testing.T) {
	t.Parallel()

	const workosUserID = "user_01LOGOUT"

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			workosUserID: {
				{ID: "om_LOGOUT", UserID: workosUserID, OrganizationID: "org_01LOGOUT", Organization: "Logout Corp", RoleSlug: "admin"},
			},
		},
		orgs: map[string]*workos.Organization{
			"org_01LOGOUT": {ID: "org_01LOGOUT", Name: "Logout Corp"},
		},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "logout@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	// Step 1: Login via callback
	callbackResult, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotContains(t, callbackResult.Location, "signin_error=")

	// Step 2: Authenticate with session
	ctx, err = inst.sessionManager.Authenticate(ctx, callbackResult.SessionToken)
	require.NoError(t, err)

	// Step 3: Logout
	logoutResult, err := inst.service.Logout(ctx, &gen.LogoutPayload{})
	require.NoError(t, err)
	require.NotNil(t, logoutResult)
	assert.Empty(t, logoutResult.SessionCookie, "logout should clear session cookie")

	// Step 4: Verify session is invalidated — Authenticate should fail
	_, err = inst.sessionManager.Authenticate(ctx, callbackResult.SessionToken)
	assert.Error(t, err, "session should be invalidated after logout")
}

// TestE2E_Register_ZeroOrgUserCreatesOrg exercises the full onboarding flow:
// callback with zero orgs → register a new org → info returns the new org.
func TestE2E_Register_ZeroOrgUserCreatesOrg(t *testing.T) {
	t.Parallel()

	const workosUserID = "user_01REGISTER"

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{},
		orgs:    map[string]*workos.Organization{},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "register@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	// Step 1: Callback — zero orgs, gets session but no active org
	callbackResult, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotContains(t, callbackResult.Location, "signin_error=")
	require.NotEmpty(t, callbackResult.SessionToken)

	// Step 2: Authenticate
	ctx, err = inst.sessionManager.Authenticate(ctx, callbackResult.SessionToken)
	require.NoError(t, err)

	// Step 3: Register — creates org and assigns it as active
	err = inst.service.Register(ctx, &gen.RegisterPayload{OrgName: "My New Org"})
	require.NoError(t, err)

	// Step 4: Re-authenticate (Register updates the session with the new org)
	ctx, err = inst.sessionManager.Authenticate(ctx, callbackResult.SessionToken)
	require.NoError(t, err)

	// Step 5: Info — should show the newly created org
	infoResult, err := inst.service.Info(ctx, &gen.InfoPayload{})
	require.NoError(t, err)
	require.NotNil(t, infoResult)
	assert.Equal(t, users.UserIDFromWorkOSID(workosUserID), infoResult.UserID)
	require.Len(t, infoResult.Organizations, 1)
	assert.Equal(t, "My New Org", infoResult.Organizations[0].Name)
	assert.Equal(t, "my-new-org", infoResult.Organizations[0].Slug)
	assert.NotEmpty(t, infoResult.ActiveOrganizationID)
	assert.NotEmpty(t, infoResult.Organizations[0].Projects, "default project should be auto-created")
}

// TestE2E_Register_RejectsWhenOrgAlreadyActive verifies that Register returns
// an error when the user already has an active organization.
func TestE2E_Register_RejectsWhenOrgAlreadyActive(t *testing.T) {
	t.Parallel()

	const workosUserID = "user_01REG_REJECT"

	fetcher := &mockWorkOSFetcher{
		members: map[string][]workos.Member{
			workosUserID: {
				{ID: "om_REJ", UserID: workosUserID, OrganizationID: "org_01REJ", Organization: "Existing", RoleSlug: "admin"},
			},
		},
		orgs: map[string]*workos.Organization{
			"org_01REJ": {ID: "org_01REJ", Name: "Existing"},
		},
	}

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "reject@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, fetcher)

	// Callback — user gets an org via WorkOS fallback
	callbackResult, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotContains(t, callbackResult.Location, "signin_error=")

	ctx, err = inst.sessionManager.Authenticate(ctx, callbackResult.SessionToken)
	require.NoError(t, err)

	// Register should fail — user already has an active org
	err = inst.service.Register(ctx, &gen.RegisterPayload{OrgName: "Should Fail"})
	assert.Error(t, err, "register should reject when user already has active org")
}

// TestE2E_Register_RejectsInvalidOrgName verifies that Register rejects org
// names with invalid characters.
func TestE2E_Register_RejectsInvalidOrgName(t *testing.T) {
	t.Parallel()

	const workosUserID = "user_01REG_INVALID"

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "invalid@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, nil)

	// Callback — zero orgs
	callbackResult, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)

	ctx, err = inst.sessionManager.Authenticate(ctx, callbackResult.SessionToken)
	require.NoError(t, err)

	// Register with invalid characters
	err = inst.service.Register(ctx, &gen.RegisterPayload{OrgName: "Bad<>Org!"})
	assert.Error(t, err, "register should reject invalid org name characters")
}

// TestE2E_FullOnboardingFlow exercises the complete new-user journey end to end:
// Login → Callback (zero orgs) → Register → SwitchScopes (no-op, same org) → Info.
func TestE2E_FullOnboardingFlow(t *testing.T) {
	t.Parallel()

	const workosUserID = "user_01FULL_ONBOARD"

	userInfo := &MockUserInfo{
		UserID:        workosUserID,
		Email:         "onboard@example.com",
		Organizations: []MockOrganizationEntry{},
	}

	ctx, inst := newE2EAuthService(t, userInfo, nil)

	// Step 1: Login — get authorization URL
	loginResult, err := inst.service.Login(ctx, &gen.LoginPayload{})
	require.NoError(t, err)
	assert.Contains(t, loginResult.Location, "/authorize")

	// Step 2: Callback — simulates redirect back with code
	callbackResult, err := inst.service.Callback(ctx, &gen.CallbackPayload{Code: "mock_code"})
	require.NoError(t, err)
	require.NotContains(t, callbackResult.Location, "signin_error=")
	require.NotEmpty(t, callbackResult.SessionToken)

	// Step 3: Authenticate
	ctx, err = inst.sessionManager.Authenticate(ctx, callbackResult.SessionToken)
	require.NoError(t, err)

	// Step 4: Register org
	err = inst.service.Register(ctx, &gen.RegisterPayload{OrgName: "Onboarding Corp"})
	require.NoError(t, err)

	// Step 5: Re-authenticate (session updated by Register)
	ctx, err = inst.sessionManager.Authenticate(ctx, callbackResult.SessionToken)
	require.NoError(t, err)

	// Step 6: Info — verify org is visible
	infoResult, err := inst.service.Info(ctx, &gen.InfoPayload{})
	require.NoError(t, err)
	require.Len(t, infoResult.Organizations, 1)
	assert.Equal(t, "Onboarding Corp", infoResult.Organizations[0].Name)
	assert.NotEmpty(t, infoResult.Organizations[0].Projects)

	// Step 7: SwitchScopes to same org (no-op, should succeed)
	orgID := infoResult.Organizations[0].ID
	switchResult, err := inst.service.SwitchScopes(ctx, &gen.SwitchScopesPayload{OrganizationID: &orgID})
	require.NoError(t, err)
	require.NotNil(t, switchResult)

	// Step 8: Logout
	ctx, err = inst.sessionManager.Authenticate(ctx, switchResult.SessionToken)
	require.NoError(t, err)
	logoutResult, err := inst.service.Logout(ctx, &gen.LogoutPayload{})
	require.NoError(t, err)
	assert.Empty(t, logoutResult.SessionCookie)
}
