package organizations_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	svix "github.com/svix/svix-webhooks/go"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/svix/svixtest"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
)

// seedLocalRole inserts an organization_roles row and returns its Gram local
// UUID — the same identifier the dashboard receives from access.listRoles and
// sends back in invite payloads.
func seedLocalRole(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, slug, name string) string {
	t.Helper()

	now := time.Now().UTC()
	_, err := accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        slug,
		WorkosName:        name,
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	row, err := accessrepo.New(conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: organizationID,
		WorkosSlug:     slug,
	})
	require.NoError(t, err)

	return row.ID.String()
}

// orgFeatureStub mirrors the unexported feature-checker interface accepted by
// organizations.NewService so test constructors can parametrize it.
type orgFeatureStub interface {
	IsFeatureEnabledUncached(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// stubOrgFeaturesEnabled reports every feature enabled, entitling all
// feature-gated portal-link intents.
type stubOrgFeaturesEnabled struct{}

func (stubOrgFeaturesEnabled) IsFeatureEnabledUncached(context.Context, string, productfeatures.Feature) (bool, error) {
	return true, nil
}

// featureMapStub enables exactly the features mapped to true.
type featureMapStub map[productfeatures.Feature]bool

func (m featureMapStub) IsFeatureEnabledUncached(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	return m[feature], nil
}

// enabledFeatures builds a featureMapStub that enables exactly the listed features.
func enabledFeatures(features ...productfeatures.Feature) featureMapStub {
	m := make(featureMapStub, len(features))
	for _, feature := range features {
		m[feature] = true
	}
	return m
}

// withAccountType returns a context whose auth context is a copy carrying the
// given account type. The copy matters: contexts share the underlying auth
// context pointer, so mutating it in place would retier every context derived
// from ctx.
func withAccountType(t *testing.T, ctx context.Context, accountType string) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	clone := *authCtx
	clone.AccountType = accountType

	return contextvalues.SetAuthContext(ctx, &clone)
}

// testAuthUserWorkOSID is the WorkOS user id for the session user in tests.
const testAuthUserWorkOSID = "user_01WORKOS_INVITER"

var (
	infra *testenv.Environment
)

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

type testInstance struct {
	service *organizations.Service
	conn    *pgxpool.Pool
	orgs    *MockOrganizationProvider
	loops   *MockLoopsClient
	trial   *fakeTrialNotifier
	posthog *fakeOnboardingTelemetry
	svixSrv *svixtest.MockServer
}

type fakeTrialNotifier struct {
	mu              sync.Mutex
	trialStartedErr error
	trialStarted    []string
	adminAddedErr   error
	adminAdded      []adminAddedNotification
}

type adminAddedNotification struct {
	organizationID string
	userID         string
}

func (f *fakeTrialNotifier) TrialStarted(_ context.Context, organizationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trialStarted = append(f.trialStarted, organizationID)
	return f.trialStartedErr
}

func (f *fakeTrialNotifier) AdminAdded(_ context.Context, organizationID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adminAdded = append(f.adminAdded, adminAddedNotification{organizationID: organizationID, userID: userID})
	return f.adminAddedErr
}

func (f *fakeTrialNotifier) TrialInactive(context.Context, string) error {
	return nil
}

type capturedOnboardingEvent struct {
	eventName  string
	distinctID string
	properties map[string]any
}

type fakeOnboardingTelemetry struct {
	mu          sync.Mutex
	captureErr  error
	identifyErr error
	events      []capturedOnboardingEvent
	identified  []capturedOnboardingEvent
}

func (f *fakeOnboardingTelemetry) CaptureEvent(_ context.Context, eventName, distinctID string, properties map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, capturedOnboardingEvent{eventName: eventName, distinctID: distinctID, properties: properties})
	return f.captureErr
}

func (f *fakeOnboardingTelemetry) IdentifyUser(_ context.Context, distinctID string, properties map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.identified = append(f.identified, capturedOnboardingEvent{distinctID: distinctID, properties: properties})
	return f.identifyErr
}

func newTestOrganizationsService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	return newTestOrganizationsServiceWithOptions(t, enabledFeatures(), productfeatures.SeedEnterpriseTrialBundleTx, stubUserProvisioner{})
}

func newTestOrganizationsServiceWithTrialBundleSeeder(t *testing.T, trialBundleSeeder auth.EnterpriseTrialBundleSeeder) (context.Context, *testInstance) {
	t.Helper()

	return newTestOrganizationsServiceWithOptions(t, enabledFeatures(), trialBundleSeeder, stubUserProvisioner{})
}

func newTestOrganizationsServiceWithInviteIdentityProvider(t *testing.T, invite organizations.InviteIdentityProvider) (context.Context, *testInstance) {
	t.Helper()

	return newTestOrganizationsServiceWithOptions(t, enabledFeatures(), productfeatures.SeedEnterpriseTrialBundleTx, invite)
}

// newTestOrganizationsServiceRBAC creates a service instance whose feature
// checker reports every feature enabled. The checker only gates portal-link
// intent entitlements, so that is the sole difference from
// newTestOrganizationsService.
func newTestOrganizationsServiceRBAC(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	return newTestOrganizationsServiceWithFeatures(t, stubOrgFeaturesEnabled{})
}

func newTestOrganizationsServiceWithFeatures(t *testing.T, features orgFeatureStub) (context.Context, *testInstance) {
	t.Helper()

	return newTestOrganizationsServiceWithOptions(t, features, productfeatures.SeedEnterpriseTrialBundleTx, stubUserProvisioner{})
}

func newTestOrganizationsServiceWithOptions(t *testing.T, features orgFeatureStub, trialBundleSeeder auth.EnterpriseTrialBundleSeeder, invite organizations.InviteIdentityProvider) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// UpsertUserFromIDP (called inside InitAuthContext) now backfills workos_id
	// with the mock IDP's user ID. Override it to the test-specific WorkOS user
	// ID so that mock expectations on GetOrgMembership match.
	err = userrepo.New(conn).OverwriteUserWorkosID(ctx, userrepo.OverwriteUserWorkosIDParams{
		ID:       authCtx.UserID,
		WorkosID: conv.ToPGText(testAuthUserWorkOSID),
	})
	require.NoError(t, err)

	orgs := newMockOrganizationProvider(t)

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, thirdpartyworkos.NewStubClient())

	auditLogger := audit.NewLogger()

	svixSrv := svixtest.NewMockServer(logger)
	t.Cleanup(svixSrv.Close)
	svixClient, err := svix.New("test-token", &svix.SvixOptions{ServerUrl: svixSrv.URL()})
	require.NoError(t, err)

	trialNotifier := &fakeTrialNotifier{}
	posthog := &fakeOnboardingTelemetry{}
	svc := organizations.NewService(logger, tracerProvider, conn, sessionManager, orgs, invite, features, nil, authzEngine, nil, trialNotifier, trialBundleSeeder, posthog, "http://localhost:35291", "http://localhost:5173", auditLogger, svixClient)

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
		orgs:    orgs,
		trial:   trialNotifier,
		posthog: posthog,
		svixSrv: svixSrv,
	}
}

// newTestOrganizationsServiceWithEmail creates a service with a mock email
// sender so tests can assert on email delivery success/failure paths.
func newTestOrganizationsServiceWithEmail(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	err = userrepo.New(conn).OverwriteUserWorkosID(ctx, userrepo.OverwriteUserWorkosIDParams{
		ID:       authCtx.UserID,
		WorkosID: conv.ToPGText(testAuthUserWorkOSID),
	})
	require.NoError(t, err)

	orgs := newMockOrganizationProvider(t)
	loopsMock := newMockLoopsClient(t)

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, thirdpartyworkos.NewStubClient())

	auditLogger := audit.NewLogger()

	svixSrv := svixtest.NewMockServer(logger)
	t.Cleanup(svixSrv.Close)
	svixClient, err := svix.New("test-token", &svix.SvixOptions{ServerUrl: svixSrv.URL()})
	require.NoError(t, err)

	emailService := email.NewService(logger, loopsMock, email.NewTemplateIDs(map[string]string{
		"team_invite": "team-invite-test-id",
	}), true)
	trialNotifier := &fakeTrialNotifier{}
	svc := organizations.NewService(logger, tracerProvider, conn, sessionManager, orgs, stubUserProvisioner{}, enabledFeatures(), nil, authzEngine, emailService, trialNotifier, productfeatures.SeedEnterpriseTrialBundleTx, nil, "http://localhost:35291", "http://localhost:5173", auditLogger, svixClient)

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
		orgs:    orgs,
		loops:   loopsMock,
		trial:   trialNotifier,
		svixSrv: svixSrv,
	}
}
