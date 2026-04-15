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

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/mock-speakeasy-idp"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
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

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	idpCfg := mockidp.NewConfig()
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

	// Seed org metadata so SetOrgWorkosID has a row to update.
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(context.Background(), orgRepo.UpsertOrganizationMetadataParams{
		ID:              mockidp.MockOrgID,
		Name:            mockidp.MockOrgName,
		Slug:            mockidp.MockOrgSlug,
		SsoConnectionID: pgtype.Text{Valid: false},
		Whitelisted:     pgtype.Bool{Valid: false},
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

// TestSyncWorkOSIDs_PopulatesNonSSOOrg is the core regression test for the bug:
// workos_id must be backfilled for orgs without an SSO connection.
func TestSyncWorkOSIDs_PopulatesNonSSOOrg(t *testing.T) {
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
	idToken := acquireIDToken(t, ctx, ts.mgr)

	// syncWorkOSIDs is called synchronously inside GetUserInfoFromSpeakeasy.
	_, err := ts.mgr.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)

	org, err := orgRepo.New(ts.conn).GetOrganizationMetadata(ctx, workosOrgID)
	require.NoError(t, err)
	require.True(t, org.WorkosID.Valid, "workos_id should be populated for non-SSO org")
	require.Equal(t, workosOrgID, org.WorkosID.String)
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
// membership for the org the workos_id stays NULL and no error propagates.
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

	org, err := orgRepo.New(ts.conn).GetOrganizationMetadata(ctx, mockidp.MockOrgID)
	require.NoError(t, err)
	require.False(t, org.WorkosID.Valid, "workos_id should remain NULL when no membership exists")
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
