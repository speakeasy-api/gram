package usage

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/trialemails"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

// --- mock billing.Repository ---

type mockBillingRepo struct {
	mock.Mock
}

func (m *mockBillingRepo) GetStoredPeriodUsage(ctx context.Context, orgID string) (*gen.PeriodUsage, error) {
	args := m.Called(ctx, orgID)
	if args.Get(0) == nil {
		return nil, fmt.Errorf("mock: %w", args.Error(1))
	}
	pu, ok := args.Get(0).(*gen.PeriodUsage)
	if !ok {
		return nil, fmt.Errorf("mock: unexpected type %T", args.Get(0))
	}
	return pu, nil
}

func (m *mockBillingRepo) GetPeriodUsage(ctx context.Context, orgID string) (*gen.PeriodUsage, error) {
	args := m.Called(ctx, orgID)
	if args.Get(0) == nil {
		return nil, fmt.Errorf("mock: %w", args.Error(1))
	}
	pu, ok := args.Get(0).(*gen.PeriodUsage)
	if !ok {
		return nil, fmt.Errorf("mock: unexpected type %T", args.Get(0))
	}
	return pu, nil
}

func (m *mockBillingRepo) GetCustomer(ctx context.Context, orgID string) (*billing.Customer, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockBillingRepo) GetCustomerTier(ctx context.Context, orgID string) (*billing.Tier, bool, error) {
	return nil, false, fmt.Errorf("not implemented")
}

func (m *mockBillingRepo) CreateCheckout(ctx context.Context, orgID, serverURL, successURL string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (m *mockBillingRepo) CreateTopUpCheckout(ctx context.Context, orgID, serverURL, successURL string) (string, error) {
	args := m.Called(ctx, orgID, serverURL, successURL)
	return args.String(0), args.Error(1)
}

func (m *mockBillingRepo) IsTopUpProductID(productID string) bool {
	return false
}

func (m *mockBillingRepo) CreateCustomerSession(ctx context.Context, orgID string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (m *mockBillingRepo) GetUsageTiers(ctx context.Context) (*gen.UsageTiers, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockBillingRepo) ValidateAndParseWebhookEvent(ctx context.Context, payload []byte, header http.Header) (*billing.PolarWebhookPayload, error) {
	args := m.Called(ctx, payload, header)
	if args.Get(0) == nil {
		return nil, fmt.Errorf("mock: %w", args.Error(1))
	}
	wp, ok := args.Get(0).(*billing.PolarWebhookPayload)
	if !ok {
		return nil, fmt.Errorf("mock: unexpected type %T", args.Get(0))
	}
	return wp, args.Error(1)
}

func (m *mockBillingRepo) InvalidateBillingCustomerCaches(ctx context.Context, orgID string) error {
	args := m.Called(ctx, orgID)
	return args.Error(0)
}

func (m *mockBillingRepo) AttachAssistantsBenefit(ctx context.Context, orgID string, email string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (m *mockBillingRepo) CancelSubscriptionAtPeriodEnd(ctx context.Context, subscriptionID string) error {
	return fmt.Errorf("not implemented")
}

var _ billing.Repository = (*mockBillingRepo)(nil)

// --- test helpers ---

func mustParseURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}

func newTestService(t *testing.T, billingRepo billing.Repository, orgID string, serverCount int) *Service {
	t.Helper()
	logger := testenv.NewLogger(t)
	tp := testenv.NewTracerProvider(t)
	db, err := infra.CloneTestDatabase(t, "usage")
	require.NoError(t, err)
	seedEnabledToolsets(t, db, orgID, serverCount)

	authzEngine := authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	return &Service{
		tracer:        tp.Tracer("test"),
		logger:        logger,
		authz:         authzEngine,
		repo:          repo.New(db),
		billingRepo:   billingRepo,
		orgRepo:       orgRepo.New(db),
		posthogClient: posthog.New(t.Context(), logger, "", "", ""),
		stripeClient:  nil,
		stripeHandler: nil,
		trial:         trialemails.NoopNotifier{},
	}
}

type fakeTrialNotifier struct {
	inactive    []string
	inactiveErr error
}

func (f *fakeTrialNotifier) TrialStarted(context.Context, string) error { return nil }

func (f *fakeTrialNotifier) AdminAdded(context.Context, string, string) error {
	return nil
}

func (f *fakeTrialNotifier) TrialInactive(_ context.Context, organizationID string) error {
	f.inactive = append(f.inactive, organizationID)
	return f.inactiveErr
}

func seedEnabledToolsets(t *testing.T, db repo.DBTX, orgID string, serverCount int) {
	t.Helper()
	ctx := t.Context()
	projectID := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO projects (id, organization_id, name, slug)
		VALUES ($1, $2, 'Usage Test Project', $3)
	`, projectID, orgID, "usage-"+projectID.String()[:8])
	require.NoError(t, err)

	for i := range serverCount {
		toolsetID := uuid.New()
		_, err = db.Exec(ctx, `
			INSERT INTO toolsets (id, organization_id, project_id, name, slug, mcp_enabled)
			VALUES ($1, $2, $3, $4, $5, TRUE)
		`, toolsetID, orgID, projectID, "Enabled Server", fmt.Sprintf("enabled-%d-%s", i, toolsetID.String()[:8]))
		require.NoError(t, err)
	}
}

func testAuthContext(orgID string) context.Context {
	ctx := context.Background()
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		AccountType:          "free",
	})
}

func sampleUsage(toolCalls, servers, credits int) *gen.PeriodUsage {
	includedCredits := 25
	return &gen.PeriodUsage{
		ToolCalls:                toolCalls,
		IncludedToolCalls:        10000,
		Servers:                  servers,
		IncludedServers:          3,
		Credits:                  &credits,
		IncludedCredits:          &includedCredits,
		HasActiveSubscription:    false,
		ActualEnabledServerCount: 0,
	}
}

// --- tests ---

func TestGetPeriodUsage_CacheHit(t *testing.T) {
	t.Parallel()
	orgID := "org-cache-hit"
	cached := sampleUsage(42, 2, 10)

	billingMock := &mockBillingRepo{}
	billingMock.On("GetStoredPeriodUsage", mock.Anything, orgID).Return(cached)
	// GetPeriodUsage should NOT be called
	svc := newTestService(t, billingMock, orgID, 5)

	ctx := testAuthContext(orgID)
	result, err := svc.GetPeriodUsage(ctx, &gen.GetPeriodUsagePayload{})

	require.NoError(t, err)
	require.Equal(t, 42, result.ToolCalls)
	require.Equal(t, 2, result.Servers)
	require.Nil(t, result.Credits)
	require.Nil(t, result.IncludedCredits)
	require.Equal(t, 5, result.ActualEnabledServerCount) // from DB, not cache
	billingMock.AssertNotCalled(t, "GetPeriodUsage", mock.Anything, mock.Anything)
}

func TestGetPeriodUsage_CacheMissFallback(t *testing.T) {
	t.Parallel()
	orgID := "org-cache-miss"
	fresh := sampleUsage(100, 5, 20)

	billingMock := &mockBillingRepo{}
	billingMock.On("GetStoredPeriodUsage", mock.Anything, orgID).Return(nil, fmt.Errorf("cache miss"))
	billingMock.On("GetPeriodUsage", mock.Anything, orgID).Return(fresh)
	svc := newTestService(t, billingMock, orgID, 3)

	ctx := testAuthContext(orgID)
	result, err := svc.GetPeriodUsage(ctx, &gen.GetPeriodUsagePayload{})

	require.NoError(t, err)
	require.Equal(t, 100, result.ToolCalls)
	require.Equal(t, 5, result.Servers)
	require.Nil(t, result.Credits)
	require.Nil(t, result.IncludedCredits)
	require.Equal(t, 3, result.ActualEnabledServerCount)
	billingMock.AssertCalled(t, "GetPeriodUsage", mock.Anything, orgID)
}

func TestGetPeriodUsage_BothFail(t *testing.T) {
	t.Parallel()
	orgID := "org-both-fail"

	billingMock := &mockBillingRepo{}
	billingMock.On("GetStoredPeriodUsage", mock.Anything, orgID).Return(nil, fmt.Errorf("cache miss"))
	billingMock.On("GetPeriodUsage", mock.Anything, orgID).Return(nil, fmt.Errorf("polar API down"))
	svc := newTestService(t, billingMock, orgID, 0)

	ctx := testAuthContext(orgID)
	_, err := svc.GetPeriodUsage(ctx, &gen.GetPeriodUsagePayload{})

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
}

func TestGetPeriodUsage_NoAuthContext(t *testing.T) {
	t.Parallel()

	billingMock := &mockBillingRepo{}
	svc := newTestService(t, billingMock, "org-no-auth", 0)

	// Empty context — no auth
	_, err := svc.GetPeriodUsage(context.Background(), &gen.GetPeriodUsagePayload{})

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestGetPeriodUsage_CreditsPlatformAdminOnly(t *testing.T) {
	t.Parallel()
	orgID := "org-credits-admin-only"
	cached := sampleUsage(42, 2, 10)

	billingMock := &mockBillingRepo{}
	billingMock.On("GetStoredPeriodUsage", mock.Anything, orgID).Return(cached)
	svc := newTestService(t, billingMock, orgID, 1)

	memberResult, err := svc.GetPeriodUsage(testAuthContext(orgID), &gen.GetPeriodUsagePayload{})
	require.NoError(t, err)
	require.Equal(t, 42, memberResult.ToolCalls)
	require.Equal(t, 2, memberResult.Servers)
	require.Nil(t, memberResult.Credits)
	require.Nil(t, memberResult.IncludedCredits)

	adminResult, err := svc.GetPeriodUsage(testAdminAuthContext(orgID), &gen.GetPeriodUsagePayload{})
	require.NoError(t, err)
	require.Equal(t, 42, adminResult.ToolCalls)
	require.NotNil(t, adminResult.Credits)
	require.Equal(t, 10, *adminResult.Credits)
	require.NotNil(t, adminResult.IncludedCredits)
	require.Equal(t, 25, *adminResult.IncludedCredits)
}

func TestGetPeriodUsage_ActualServerCountFromDB(t *testing.T) {
	t.Parallel()
	orgID := "org-server-count"
	cached := sampleUsage(0, 0, 0)
	cached.ActualEnabledServerCount = 999 // cached value should be overridden

	billingMock := &mockBillingRepo{}
	billingMock.On("GetStoredPeriodUsage", mock.Anything, orgID).Return(cached)
	svc := newTestService(t, billingMock, orgID, 7) // DB says 7

	ctx := testAuthContext(orgID)
	result, err := svc.GetPeriodUsage(ctx, &gen.GetPeriodUsagePayload{})

	require.NoError(t, err)
	require.Equal(t, 7, result.ActualEnabledServerCount, "should use DB count, not cached value")
}

func TestHandlePolarWebhook_OrgNotFound(t *testing.T) {
	t.Parallel()
	orgID := "org-does-not-exist"

	payload := &billing.PolarWebhookPayload{Type: "subscription.created"}
	payload.Data.Customer = &billing.WebhookCustomer{ExternalID: orgID}

	billingMock := &mockBillingRepo{}
	billingMock.On("ValidateAndParseWebhookEvent", mock.Anything, mock.Anything, mock.Anything).
		Return(payload, nil)
	svc := newTestService(t, billingMock, orgID, 0)

	req := httptest.NewRequest(http.MethodPost, "/rpc/polar.webhook", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	err := svc.HandlePolarWebhook(rec, req)

	require.Error(t, err)
	var se *oops.ShareableError
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeNotFound, se.Code,
		"a webhook for an unknown polar external id should map to CodeNotFound, not CodeUnexpected")
	// We never get past the org lookup, so no cache invalidation should occur.
	billingMock.AssertNotCalled(t, "InvalidateBillingCustomerCaches", mock.Anything, mock.Anything)
}

func seedWebhookOrg(t *testing.T, svc *Service, orgID string) {
	t.Helper()

	_, err := svc.orgRepo.UpsertOrganizationMetadata(t.Context(), orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Trial Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, svc.orgRepo.SetAccountType(t.Context(), orgRepo.SetAccountTypeParams{
		ID:              orgID,
		GramAccountType: "enterprise",
	}))
}

func newSubscriptionWebhookService(t *testing.T, orgID, eventType string) (*Service, *fakeTrialNotifier, *mockBillingRepo) {
	t.Helper()

	payload := &billing.PolarWebhookPayload{Type: eventType}
	payload.Data.Customer = &billing.WebhookCustomer{
		ExternalID: orgID,
		Name:       "Trial Org",
		Email:      "person@example.com",
	}

	billingMock := &mockBillingRepo{}
	billingMock.On("ValidateAndParseWebhookEvent", mock.Anything, mock.Anything, mock.Anything).
		Return(payload, nil)
	billingMock.On("InvalidateBillingCustomerCaches", mock.Anything, orgID).Return(nil)
	billingMock.On("GetPeriodUsage", mock.Anything, orgID).Return(sampleUsage(0, 0, 0), nil)

	svc := newTestService(t, billingMock, orgID, 0)
	seedWebhookOrg(t, svc, orgID)
	notifier := &fakeTrialNotifier{}
	svc.trial = notifier
	return svc, notifier, billingMock
}

func TestHandlePolarWebhook_SubscriptionCreatedMarksTrialInactive(t *testing.T) {
	t.Parallel()

	orgID := "org-trial-converted"
	svc, notifier, _ := newSubscriptionWebhookService(t, orgID, "subscription.created")

	req := httptest.NewRequest(http.MethodPost, "/rpc/polar.webhook", strings.NewReader("{}"))
	require.NoError(t, svc.HandlePolarWebhook(httptest.NewRecorder(), req))
	require.Equal(t, []string{orgID}, notifier.inactive)
}

func TestHandlePolarWebhook_SubscriptionActiveMarksTrialInactive(t *testing.T) {
	t.Parallel()

	orgID := "org-trial-active"
	svc, notifier, _ := newSubscriptionWebhookService(t, orgID, "subscription.active")

	req := httptest.NewRequest(http.MethodPost, "/rpc/polar.webhook", strings.NewReader("{}"))
	require.NoError(t, svc.HandlePolarWebhook(httptest.NewRecorder(), req))
	require.Equal(t, []string{orgID}, notifier.inactive)
}

func TestHandlePolarWebhook_SubscriptionCanceledDoesNotMarkTrialInactive(t *testing.T) {
	t.Parallel()

	orgID := "org-trial-canceled"
	svc, notifier, _ := newSubscriptionWebhookService(t, orgID, "subscription.canceled")

	req := httptest.NewRequest(http.MethodPost, "/rpc/polar.webhook", strings.NewReader("{}"))
	require.NoError(t, svc.HandlePolarWebhook(httptest.NewRecorder(), req))
	require.Empty(t, notifier.inactive)
}

func TestHandlePolarWebhook_TrialInactiveFailureDoesNotFailWebhook(t *testing.T) {
	t.Parallel()

	orgID := "org-trial-notify-fail"
	svc, notifier, _ := newSubscriptionWebhookService(t, orgID, "subscription.created")
	notifier.inactiveErr = fmt.Errorf("loops unavailable")

	req := httptest.NewRequest(http.MethodPost, "/rpc/polar.webhook", strings.NewReader("{}"))
	require.NoError(t, svc.HandlePolarWebhook(httptest.NewRecorder(), req))
	require.Equal(t, []string{orgID}, notifier.inactive)
}

func TestCreateTopUpCheckout_BillingErrorMapsToCodeUnexpected(t *testing.T) {
	t.Parallel()
	orgID := "org-topup"

	billingMock := &mockBillingRepo{}
	billingMock.On("CreateTopUpCheckout", mock.Anything, orgID, mock.Anything, mock.Anything).
		Return("", fmt.Errorf("polar API down"))

	svc := newTestService(t, billingMock, orgID, 0)
	svc.serverURL = mustParseURL(t, "https://example.test")

	ctx := testAuthContext(orgID)
	_, err := svc.CreateTopUpCheckout(ctx, &gen.CreateTopUpCheckoutPayload{})

	require.Error(t, err)
	var se *oops.ShareableError
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeUnexpected, se.Code,
		"billing provider failures (config / Polar API) are server-side, not client errors")
}
