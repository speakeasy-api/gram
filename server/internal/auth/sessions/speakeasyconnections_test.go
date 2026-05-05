package sessions_test

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

// fakeWorkOSServer is a minimal fake of the WorkOS API that handles the two
// endpoints called by syncWorkOSIDs: user list (for GetUserByEmail) and
// org membership list (for ListUserMemberships).
type fakeWorkOSServer struct {
	mu          sync.Mutex
	users       []fakeWOSUser
	memberships []fakeWOSMembership
	userLookups int // counts email-lookup calls; used to assert caching
}

type fakeWOSUser struct {
	ID    string
	Email string
}

type fakeWOSMembership struct {
	ID             string
	UserID         string
	OrganizationID string
	RoleSlug       string
}

func newFakeWorkOSServer() *fakeWorkOSServer {
	return &fakeWorkOSServer{}
}

func (f *fakeWorkOSServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/user_management/users":
		f.handleListUsers(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/user_management/organization_memberships":
		f.handleListMemberships(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeWorkOSServer) handleListUsers(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")

	f.mu.Lock()
	if email != "" {
		f.userLookups++
	}
	var data []map[string]any
	for _, u := range f.users {
		if email == "" || u.Email == email {
			data = append(data, map[string]any{"id": u.ID, "email": u.Email})
		}
	}
	f.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data":          data,
		"list_metadata": map[string]string{"before": "", "after": ""},
	})
}

func (f *fakeWorkOSServer) handleListMemberships(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	orgID := r.URL.Query().Get("organization_id")

	f.mu.Lock()
	var data []map[string]any
	for _, m := range f.memberships {
		if (orgID == "" || m.OrganizationID == orgID) && (userID == "" || m.UserID == userID) {
			data = append(data, map[string]any{
				"id":              m.ID,
				"user_id":         m.UserID,
				"organization_id": m.OrganizationID,
				"role":            map[string]string{"slug": m.RoleSlug},
				"status":          "active",
			})
		}
	}
	f.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data":          data,
		"list_metadata": map[string]string{"before": "", "after": ""},
	})
}

// testSetup holds the sessions manager and its DB connection so tests can
// read back DB state written by syncWorkOSIDs using the same database.
type testSetup struct {
	mgr    *sessions.Manager
	conn   *pgxpool.Pool
	idpCfg mockidp.Config
}

// newManagerWithFakeWorkOS creates a sessions.Manager backed by the mock IDP
// and a fake WorkOS server. The mock IDP org is seeded into organization_metadata.
func newManagerWithFakeWorkOS(t *testing.T, fake *fakeWorkOSServer) *testSetup {
	t.Helper()
	return newManagerWithFakeWorkOSConfig(t, fake, mockidp.NewConfig())
}

// newManagerWithFakeWorkOSConfig is like newManagerWithFakeWorkOS but uses the
// given mock IDP config (e.g. to omit workos_id from /validate).
func newManagerWithFakeWorkOSConfig(t *testing.T, fake *fakeWorkOSServer, idpCfg mockidp.Config) *testSetup {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	idpSrv := httptest.NewServer(mockidp.Handler(idpCfg))
	t.Cleanup(idpSrv.Close)

	wosSrv := httptest.NewServer(fake)
	t.Cleanup(wosSrv.Close)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	workosClient := workos.NewClient(guardianPolicy, "test-api-key", workos.ClientOpts{
		Endpoint:   wosSrv.URL,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	})

	fakePylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)
	fakePosthog := posthog.New(context.Background(), logger, "test-key", "test-host", "")
	billingClient := billing.NewStubClient(logger, tracerProvider)

	mgr := sessions.NewManager(
		logger,
		tracerProvider,
		guardianPolicy,
		conn,
		redisClient,
		cache.Suffix("test"),
		idpSrv.URL,
		mockidp.MockSecretKey,
		fakePylon,
		fakePosthog,
		billingClient,
		workosClient,
	)

	// Seed org metadata with workos_id set so the CTE can resolve WorkOS org
	// IDs back to Speakeasy org IDs. In the mock IDP the same ID is used for both.
	oq := orgRepo.New(conn)
	_, err = oq.UpsertOrganizationMetadata(context.Background(), orgRepo.UpsertOrganizationMetadataParams{
		ID:          mockidp.MockOrgID,
		Name:        mockidp.MockOrgName,
		Slug:        mockidp.MockOrgSlug,
		WorkosID:    pgtype.Text{Valid: false},
		Whitelisted: pgtype.Bool{Valid: false},
	})
	require.NoError(t, err)
	_, err = oq.SetOrgWorkosID(context.Background(), orgRepo.SetOrgWorkosIDParams{
		WorkosID:       pgtype.Text{String: mockidp.MockOrgID, Valid: true},
		OrganizationID: mockidp.MockOrgID,
	})
	require.NoError(t, err)

	return &testSetup{mgr: mgr, conn: conn, idpCfg: idpCfg}
}

// acquireIDToken exchanges a mock auth code for an ID token.
func acquireIDToken(t *testing.T, ctx context.Context, mgr *sessions.Manager) string {
	t.Helper()
	idToken, err := mgr.ExchangeTokenFromSpeakeasy(ctx, "test-code")
	require.NoError(t, err)
	return idToken
}

// TestCallbackUpsert_PopulatesWorkOSIDForExistingOrg is the core regression test
// for the bug: workos_id must be populated from validate (workos_id) for an org
// row that previously existed without one. The callback upsert (simulated here
// by UpsertOrganizationMetadata with the validate response's WorkosID) is the
// owner of org metadata freshness under the post-PR-2246 architecture.
func TestCallbackUpsert_PopulatesWorkOSIDForExistingOrg(t *testing.T) {
	t.Parallel()

	const workosUserID = "wos_user_1"
	const workosOrgID = mockidp.MockOrgID
	const workosMemID = "mem_1"

	fake := newFakeWorkOSServer()
	fake.users = []fakeWOSUser{{ID: workosUserID, Email: mockidp.MockUserEmail}}
	fake.memberships = []fakeWOSMembership{
		{ID: workosMemID, UserID: workosUserID, OrganizationID: workosOrgID, RoleSlug: "member"},
	}

	ts := newManagerWithFakeWorkOS(t, fake)
	ctx := t.Context()

	// Simulate an org row that has not yet been linked to WorkOS in Gram.
	err := orgRepo.New(ts.conn).ClearOrganizationWorkosID(ctx, workosOrgID)
	require.NoError(t, err)

	idToken := acquireIDToken(t, ctx, ts.mgr)
	userInfo, err := ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)
	require.NotEmpty(t, userInfo.Organizations)

	// Simulate the Callback flow's upsert with the active org's validate data.
	oq := orgRepo.New(ts.conn)
	activeOrg := userInfo.Organizations[0]
	_, err = oq.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          activeOrg.ID,
		Name:        activeOrg.Name,
		Slug:        activeOrg.Slug,
		WorkosID:    conv.PtrToPGText(activeOrg.WorkosID),
		Whitelisted: pgtype.Bool{Valid: false},
	})
	require.NoError(t, err)

	org, err := oq.GetOrganizationMetadata(ctx, workosOrgID)
	require.NoError(t, err)
	require.True(t, org.WorkosID.Valid, "workos_id should be populated from validate workos_id")
	require.Equal(t, workosOrgID, org.WorkosID.String)
}

// TestSyncWorkOSIDs_SkipsSetOrgWorkosIDWhenValidateOmitsWorkOSID verifies that when
// the Speakeasy validate response omits workos_id, Gram does not set organization_metadata.workos_id.
func TestSyncWorkOSIDs_SkipsSetOrgWorkosIDWhenValidateOmitsWorkOSID(t *testing.T) {
	t.Parallel()

	cfg := mockidp.NewConfig()
	cfg.Organization.WorkOSID = nil

	const workosUserID = "wos_user_skip_validate_wos"

	fake := newFakeWorkOSServer()
	fake.users = []fakeWOSUser{{ID: workosUserID, Email: mockidp.MockUserEmail}}
	fake.memberships = []fakeWOSMembership{
		{ID: "mem_skip_v", UserID: workosUserID, OrganizationID: mockidp.MockOrgID, RoleSlug: "member"},
	}

	ts := newManagerWithFakeWorkOSConfig(t, fake, cfg)
	ctx := t.Context()

	err := orgRepo.New(ts.conn).ClearOrganizationWorkosID(ctx, mockidp.MockOrgID)
	require.NoError(t, err)

	idToken := acquireIDToken(t, ctx, ts.mgr)
	_, err = ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	org, err := orgRepo.New(ts.conn).GetOrganizationMetadata(ctx, mockidp.MockOrgID)
	require.NoError(t, err)
	require.False(t, org.WorkosID.Valid, "workos_id must remain unset when validate omits workos_id")
}

// TestUpsertAfterLogin_PreservesWorkosIDWhenIDPOmitsIt exercises the full Callback
// flow: syncWorkOSIDs sets workos_id, then UpsertOrganizationMetadata is called
// with nil WorkosID (IDP omitted it). The COALESCE in the upsert must preserve
// the existing value.
func TestUpsertAfterLogin_PreservesWorkosIDWhenIDPOmitsIt(t *testing.T) {
	t.Parallel()

	const workosUserID = "wos_user_upsert_preserve"

	fake := newFakeWorkOSServer()
	fake.users = []fakeWOSUser{{ID: workosUserID, Email: mockidp.MockUserEmail}}
	fake.memberships = []fakeWOSMembership{
		{ID: "mem_preserve", UserID: workosUserID, OrganizationID: mockidp.MockOrgID, RoleSlug: "member"},
	}

	// First login WITH workos_id to populate it.
	ts := newManagerWithFakeWorkOS(t, fake)
	ctx := t.Context()
	idToken := acquireIDToken(t, ctx, ts.mgr)

	userInfo, err := ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)
	require.NotEmpty(t, userInfo.Organizations)

	// Simulate what Callback does: upsert org metadata with the IDP value.
	oq := orgRepo.New(ts.conn)
	_, err = oq.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:       userInfo.Organizations[0].ID,
		Name:     userInfo.Organizations[0].Name,
		Slug:     userInfo.Organizations[0].Slug,
		WorkosID: pgtype.Text{String: mockidp.MockOrgID, Valid: true},
	})
	require.NoError(t, err)

	// Verify workos_id is set.
	org, err := oq.GetOrganizationMetadata(ctx, mockidp.MockOrgID)
	require.NoError(t, err)
	require.True(t, org.WorkosID.Valid)
	require.Equal(t, mockidp.MockOrgID, org.WorkosID.String)

	// Now simulate a subsequent upsert where IDP omits workos_id (nil).
	_, err = oq.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:       userInfo.Organizations[0].ID,
		Name:     userInfo.Organizations[0].Name,
		Slug:     userInfo.Organizations[0].Slug,
		WorkosID: pgtype.Text{Valid: false}, // nil — IDP omitted it
	})
	require.NoError(t, err)

	// workos_id must be preserved by the COALESCE.
	org, err = oq.GetOrganizationMetadata(ctx, mockidp.MockOrgID)
	require.NoError(t, err)
	require.True(t, org.WorkosID.Valid, "workos_id must be preserved when upsert passes NULL")
	require.Equal(t, mockidp.MockOrgID, org.WorkosID.String)
}

// TestSyncWorkOSIDs_UserWorkosIDSet verifies the user's workos_id is recorded
// so subsequent logins skip the WorkOS email lookup.
func TestSyncWorkOSIDs_UserWorkosIDSet(t *testing.T) {
	t.Parallel()

	const workosUserID = "wos_user_2"

	fake := newFakeWorkOSServer()
	fake.users = []fakeWOSUser{{ID: workosUserID, Email: mockidp.MockUserEmail}}
	fake.memberships = []fakeWOSMembership{
		{ID: "mem_1", UserID: workosUserID, OrganizationID: mockidp.MockOrgID, RoleSlug: "member"},
	}

	ts := newManagerWithFakeWorkOS(t, fake)
	ctx := t.Context()
	idToken := acquireIDToken(t, ctx, ts.mgr)

	userInfo, err := ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	user, err := userRepo.New(ts.conn).GetUser(ctx, userInfo.UserID)
	require.NoError(t, err)
	require.True(t, user.WorkosID.Valid, "user workos_id should be set after first login")
	require.Equal(t, workosUserID, user.WorkosID.String)
}

// TestSyncWorkOSIDs_CachesUserWorkosID verifies that a second login skips the
// WorkOS email lookup because the user's workos_id is already in the DB.
func TestSyncWorkOSIDs_CachesUserWorkosID(t *testing.T) {
	t.Parallel()

	const workosUserID = "wos_user_cache"

	fake := newFakeWorkOSServer()
	fake.users = []fakeWOSUser{{ID: workosUserID, Email: mockidp.MockUserEmail}}
	fake.memberships = []fakeWOSMembership{
		{ID: "mem_1", UserID: workosUserID, OrganizationID: mockidp.MockOrgID, RoleSlug: "member"},
	}

	ts := newManagerWithFakeWorkOS(t, fake)
	ctx := t.Context()
	idToken := acquireIDToken(t, ctx, ts.mgr)

	// First login — populates the user's workos_id in the DB.
	_, err := ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	// Second login — workos_id already in DB; email lookup must not repeat.
	_, err = ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	fake.mu.Lock()
	lookups := fake.userLookups
	fake.mu.Unlock()

	require.Equal(t, 1, lookups, "WorkOS email lookup should happen only once across two logins")
}

// TestSyncWorkOSIDs_NoMembershipForOrg verifies that when the user has no WorkOS
// membership for the org, no workos_membership_id is set and no error propagates.
func TestSyncWorkOSIDs_NoMembershipForOrg(t *testing.T) {
	t.Parallel()

	fake := newFakeWorkOSServer()
	fake.users = []fakeWOSUser{{ID: "wos_user_3", Email: mockidp.MockUserEmail}}
	// No memberships — user exists in WorkOS but is not a member of this org.

	ts := newManagerWithFakeWorkOS(t, fake)
	ctx := t.Context()
	idToken := acquireIDToken(t, ctx, ts.mgr)

	_, err := ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	// The org-user relationship should be soft-deleted since user has no WorkOS
	// membership and the org has a workos_id set.
	_, err = orgRepo.New(ts.conn).GetOrganizationUserRelationship(ctx, orgRepo.GetOrganizationUserRelationshipParams{
		OrganizationID: mockidp.MockOrgID,
		UserID:         mockidp.MockUserID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "relationship should be soft-deleted when no membership exists")
}

// TestSyncWorkOSIDs_UserNotInWorkOS verifies that when the user's email is
// not found in WorkOS the function returns early without surfacing an error.
func TestSyncWorkOSIDs_UserNotInWorkOS(t *testing.T) {
	t.Parallel()

	fake := newFakeWorkOSServer()
	// No users seeded — user does not exist in WorkOS.

	ts := newManagerWithFakeWorkOS(t, fake)
	ctx := t.Context()
	idToken := acquireIDToken(t, ctx, ts.mgr)

	_, err := ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)
}

// TestSyncWorkOSIDs_NilWorkOSClient verifies that when no WorkOS client is
// configured (OSS / dev mode) the sync is a silent no-op and login still succeeds.
func TestSyncWorkOSIDs_NilWorkOSClient(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	idpCfg := mockidp.NewConfig()
	idpSrv := httptest.NewServer(mockidp.Handler(idpCfg))
	t.Cleanup(idpSrv.Close)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	fakePylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)
	fakePosthog := posthog.New(context.Background(), logger, "test-key", "test-host", "")
	billingClient := billing.NewStubClient(logger, tracerProvider)

	// workos = nil simulates OSS mode with no WorkOS configured.
	mgr := sessions.NewManager(
		logger,
		tracerProvider,
		guardianPolicy,
		conn,
		redisClient,
		cache.Suffix("test"),
		idpSrv.URL,
		mockidp.MockSecretKey,
		fakePylon,
		fakePosthog,
		billingClient,
		nil,
	)

	ctx := t.Context()
	idToken, err := mgr.ExchangeTokenFromSpeakeasy(ctx, "test-code")
	require.NoError(t, err)

	_, err = mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)
}

// TestSyncWorkOSIDs_OrgAlreadyLinked verifies that re-syncing an org whose
// workos_id is already set is a no-op (SetOrgWorkosID returns pgx.ErrNoRows
// because of the WHERE workos_id IS NULL guard, which is silently swallowed).
func TestSyncWorkOSIDs_OrgAlreadyLinked(t *testing.T) {
	t.Parallel()

	const workosUserID = "wos_user_linked"

	fake := newFakeWorkOSServer()
	fake.users = []fakeWOSUser{{ID: workosUserID, Email: mockidp.MockUserEmail}}
	fake.memberships = []fakeWOSMembership{
		{ID: "mem_1", UserID: workosUserID, OrganizationID: mockidp.MockOrgID, RoleSlug: "member"},
	}

	ts := newManagerWithFakeWorkOS(t, fake)
	ctx := t.Context()
	idToken := acquireIDToken(t, ctx, ts.mgr)

	// First login links the org.
	_, err := ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	// Second login: workos_id already set; must not error.
	_, err = ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)
}

// TestSyncWorkOSIDs_ClearsStaleMemberships verifies that when a user loses a
// WorkOS membership, the workos_membership_id is cleared on the next sync.
func TestSyncWorkOSIDs_ClearsStaleMemberships(t *testing.T) {
	t.Parallel()

	const workosUserID = "wos_user_stale"

	fake := newFakeWorkOSServer()
	fake.users = []fakeWOSUser{{ID: workosUserID, Email: mockidp.MockUserEmail}}
	fake.memberships = []fakeWOSMembership{
		{ID: "mem_stale", UserID: workosUserID, OrganizationID: mockidp.MockOrgID, RoleSlug: "member"},
	}

	ts := newManagerWithFakeWorkOS(t, fake)
	ctx := t.Context()
	idToken := acquireIDToken(t, ctx, ts.mgr)

	// First login: sets workos_membership_id and org workos_id.
	_, err := ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	rel, err := orgRepo.New(ts.conn).GetOrganizationUserRelationship(ctx, orgRepo.GetOrganizationUserRelationshipParams{
		OrganizationID: mockidp.MockOrgID,
		UserID:         mockidp.MockUserID,
	})
	require.NoError(t, err)
	require.True(t, rel.WorkosMembershipID.Valid, "workos_membership_id should be set after first login")

	// Remove the membership from WorkOS (user left the org).
	fake.mu.Lock()
	fake.memberships = nil
	fake.mu.Unlock()

	// Second login: membership gone → relationship should be soft-deleted.
	_, err = ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	// GetOrganizationUserRelationship filters by deleted_at IS NULL, so a
	// soft-deleted relationship returns ErrNoRows.
	_, err = orgRepo.New(ts.conn).GetOrganizationUserRelationship(ctx, orgRepo.GetOrganizationUserRelationshipParams{
		OrganizationID: mockidp.MockOrgID,
		UserID:         mockidp.MockUserID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "relationship should be soft-deleted after membership removed")
}

// TestSyncWorkOSIDs_SkipsNullWorkOSIDOrgs verifies that orgs without a
// workos_id in organization_metadata are NOT affected by the declarative sync.
func TestSyncWorkOSIDs_SkipsNullWorkOSIDOrgs(t *testing.T) {
	t.Parallel()

	const workosUserID = "wos_user_skip"
	const otherOrgID = "550e8400-e29b-41d4-a716-000000000099"

	fake := newFakeWorkOSServer()
	fake.users = []fakeWOSUser{{ID: workosUserID, Email: mockidp.MockUserEmail}}
	// Only a membership for mockOrgID; NOT for otherOrgID.
	fake.memberships = []fakeWOSMembership{
		{ID: "mem_skip", UserID: workosUserID, OrganizationID: mockidp.MockOrgID, RoleSlug: "member"},
	}

	ts := newManagerWithFakeWorkOS(t, fake)
	ctx := t.Context()
	idToken := acquireIDToken(t, ctx, ts.mgr)

	// First login creates the user and sets up the mockOrg membership.
	_, err := ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	// Create a second org WITHOUT a workos_id.
	_, err = orgRepo.New(ts.conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          otherOrgID,
		Name:        "No WorkOS Org",
		Slug:        "no-workos-org",
		WorkosID:    pgtype.Text{Valid: false},
		Whitelisted: pgtype.Bool{Valid: false},
	})
	require.NoError(t, err)

	// Create a relationship between the user and the non-WorkOS org, with a
	// workos_membership_id manually set (simulating prior state).
	err = orgRepo.New(ts.conn).AttachWorkOSUserToOrg(ctx, orgRepo.AttachWorkOSUserToOrgParams{
		OrganizationID:     otherOrgID,
		UserID:             mockidp.MockUserID,
		WorkosMembershipID: pgtype.Text{String: "mem_other", Valid: true},
	})
	require.NoError(t, err)

	// Second login: triggers sync again; should NOT clear the non-WorkOS org's membership.
	_, err = ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	// The non-WorkOS org's membership must still have its workos_membership_id.
	rel, err := orgRepo.New(ts.conn).GetOrganizationUserRelationship(ctx, orgRepo.GetOrganizationUserRelationshipParams{
		OrganizationID: otherOrgID,
		UserID:         mockidp.MockUserID,
	})
	require.NoError(t, err)
	require.True(t, rel.WorkosMembershipID.Valid, "workos_membership_id on org without workos_id must not be cleared")
	require.Equal(t, "mem_other", rel.WorkosMembershipID.String)
}

// TestSyncWorkOSIDs_DoesNotAffectOtherUsers verifies that syncing user 1's
// WorkOS memberships does not modify user 2's workos_membership_id.
func TestSyncWorkOSIDs_DoesNotAffectOtherUsers(t *testing.T) {
	t.Parallel()

	const workosUserID = "wos_user_isolation"
	const otherUserID = "other-user-999"

	fake := newFakeWorkOSServer()
	fake.users = []fakeWOSUser{{ID: workosUserID, Email: mockidp.MockUserEmail}}
	// User 1 has NO WorkOS memberships for this org.
	fake.memberships = nil

	ts := newManagerWithFakeWorkOS(t, fake)
	ctx := t.Context()

	// Seed user 2 in the users table.
	_, err := userRepo.New(ts.conn).UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          otherUserID,
		Email:       "other@example.com",
		DisplayName: "Other User",
		Admin:       false,
	})
	require.NoError(t, err)

	// workos_id is already set by the test setup; give user 2 a workos_membership_id.
	err = orgRepo.New(ts.conn).AttachWorkOSUserToOrg(ctx, orgRepo.AttachWorkOSUserToOrgParams{
		OrganizationID:     mockidp.MockOrgID,
		UserID:             otherUserID,
		WorkosMembershipID: pgtype.Text{String: "mem_user2", Valid: true},
	})
	require.NoError(t, err)

	// Login as user 1 — no WorkOS memberships → user 1's membership cleared.
	idToken := acquireIDToken(t, ctx, ts.mgr)
	_, err = ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	// User 2's membership must be untouched.
	rel, err := orgRepo.New(ts.conn).GetOrganizationUserRelationship(ctx, orgRepo.GetOrganizationUserRelationshipParams{
		OrganizationID: mockidp.MockOrgID,
		UserID:         otherUserID,
	})
	require.NoError(t, err)
	require.True(t, rel.WorkosMembershipID.Valid, "other user's workos_membership_id must not be cleared")
	require.Equal(t, "mem_user2", rel.WorkosMembershipID.String)
}

// TestSyncWorkOSIDs_UndeletesRelationship verifies that a previously
// soft-deleted organization_user_relationship is restored when the user
// regains a WorkOS membership for that org.
func TestSyncWorkOSIDs_UndeletesRelationship(t *testing.T) {
	t.Parallel()

	const workosUserID = "wos_user_undelete"

	fake := newFakeWorkOSServer()
	fake.users = []fakeWOSUser{{ID: workosUserID, Email: mockidp.MockUserEmail}}
	fake.memberships = []fakeWOSMembership{
		{ID: "mem_undel", UserID: workosUserID, OrganizationID: mockidp.MockOrgID, RoleSlug: "member"},
	}

	ts := newManagerWithFakeWorkOS(t, fake)
	ctx := t.Context()
	idToken := acquireIDToken(t, ctx, ts.mgr)

	// First login: creates the relationship.
	_, err := ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	// Remove WorkOS membership → relationship gets soft-deleted.
	fake.mu.Lock()
	fake.memberships = nil
	fake.mu.Unlock()

	_, err = ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	_, err = orgRepo.New(ts.conn).GetOrganizationUserRelationship(ctx, orgRepo.GetOrganizationUserRelationshipParams{
		OrganizationID: mockidp.MockOrgID,
		UserID:         mockidp.MockUserID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "relationship should be soft-deleted")

	// Restore the WorkOS membership → relationship should be un-deleted.
	fake.mu.Lock()
	fake.memberships = []fakeWOSMembership{
		{ID: "mem_undel", UserID: workosUserID, OrganizationID: mockidp.MockOrgID, RoleSlug: "member"},
	}
	fake.mu.Unlock()

	_, err = ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	rel, err := orgRepo.New(ts.conn).GetOrganizationUserRelationship(ctx, orgRepo.GetOrganizationUserRelationshipParams{
		OrganizationID: mockidp.MockOrgID,
		UserID:         mockidp.MockUserID,
	})
	require.NoError(t, err, "relationship should be restored after membership re-added")
	require.True(t, rel.WorkosMembershipID.Valid)
	require.Equal(t, "mem_undel", rel.WorkosMembershipID.String)
}
