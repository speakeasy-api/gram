package usage

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

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

func (m *mockBillingRepo) CreateCustomerSession(ctx context.Context, orgID string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (m *mockBillingRepo) GetUsageTiers(ctx context.Context) (*gen.UsageTiers, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockBillingRepo) ValidateAndParseWebhookEvent(ctx context.Context, payload []byte, header http.Header) (*billing.PolarWebhookPayload, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockBillingRepo) InvalidateBillingCustomerCaches(ctx context.Context, orgID string) error {
	return fmt.Errorf("not implemented")
}

var _ billing.Repository = (*mockBillingRepo)(nil)

// --- mock DBTX for repo.Queries ---

type mockRow struct {
	val int64
}

func (r *mockRow) Scan(dest ...any) error {
	if len(dest) > 0 {
		if p, ok := dest[0].(*int64); ok {
			*p = r.val
		}
	}
	return nil
}

type mockDBTX struct {
	serverCount int64
}

func (m *mockDBTX) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (m *mockDBTX) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, nil
}

func (m *mockDBTX) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return &mockRow{val: m.serverCount}
}

// --- test helpers ---

func rbacDisabled(_ context.Context, _ string) (bool, error) { return false, nil }

func newTestService(t *testing.T, billingRepo billing.Repository, serverCount int64) *Service {
	t.Helper()
	logger := testenv.NewLogger(t)
	tp := testenv.NewTracerProvider(t)
	db := &mockDBTX{serverCount: serverCount}
	authzEngine := authz.NewEngine(logger, db, rbacDisabled, workos.NewStubClient(), cache.NoopCache)

	return &Service{
		tracer:      tp.Tracer("test"),
		logger:      logger,
		authz:       authzEngine,
		repo:        repo.New(db),
		billingRepo: billingRepo,
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
	return &gen.PeriodUsage{
		ToolCalls:                toolCalls,
		IncludedToolCalls:        10000,
		Servers:                  servers,
		IncludedServers:          3,
		Credits:                  credits,
		IncludedCredits:          25,
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
	billingMock.On("GetStoredPeriodUsage", mock.Anything, orgID).Return(cached, nil)
	// GetPeriodUsage should NOT be called
	svc := newTestService(t, billingMock, 5)

	ctx := testAuthContext(orgID)
	result, err := svc.GetPeriodUsage(ctx, &gen.GetPeriodUsagePayload{})

	require.NoError(t, err)
	require.Equal(t, 42, result.ToolCalls)
	require.Equal(t, 2, result.Servers)
	require.Equal(t, 10, result.Credits)
	require.Equal(t, 5, result.ActualEnabledServerCount) // from DB, not cache
	billingMock.AssertNotCalled(t, "GetPeriodUsage", mock.Anything, mock.Anything)
}

func TestGetPeriodUsage_CacheMissFallback(t *testing.T) {
	t.Parallel()
	orgID := "org-cache-miss"
	fresh := sampleUsage(100, 5, 20)

	billingMock := &mockBillingRepo{}
	billingMock.On("GetStoredPeriodUsage", mock.Anything, orgID).Return(nil, fmt.Errorf("cache miss"))
	billingMock.On("GetPeriodUsage", mock.Anything, orgID).Return(fresh, nil)
	svc := newTestService(t, billingMock, 3)

	ctx := testAuthContext(orgID)
	result, err := svc.GetPeriodUsage(ctx, &gen.GetPeriodUsagePayload{})

	require.NoError(t, err)
	require.Equal(t, 100, result.ToolCalls)
	require.Equal(t, 5, result.Servers)
	require.Equal(t, 20, result.Credits)
	require.Equal(t, 3, result.ActualEnabledServerCount)
	billingMock.AssertCalled(t, "GetPeriodUsage", mock.Anything, orgID)
}

func TestGetPeriodUsage_BothFail(t *testing.T) {
	t.Parallel()
	orgID := "org-both-fail"

	billingMock := &mockBillingRepo{}
	billingMock.On("GetStoredPeriodUsage", mock.Anything, orgID).Return(nil, fmt.Errorf("cache miss"))
	billingMock.On("GetPeriodUsage", mock.Anything, orgID).Return(nil, fmt.Errorf("polar API down"))
	svc := newTestService(t, billingMock, 0)

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
	svc := newTestService(t, billingMock, 0)

	// Empty context — no auth
	_, err := svc.GetPeriodUsage(context.Background(), &gen.GetPeriodUsagePayload{})

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestGetPeriodUsage_ActualServerCountFromDB(t *testing.T) {
	t.Parallel()
	orgID := "org-server-count"
	cached := sampleUsage(0, 0, 0)
	cached.ActualEnabledServerCount = 999 // cached value should be overridden

	billingMock := &mockBillingRepo{}
	billingMock.On("GetStoredPeriodUsage", mock.Anything, orgID).Return(cached, nil)
	svc := newTestService(t, billingMock, 7) // DB says 7

	ctx := testAuthContext(orgID)
	result, err := svc.GetPeriodUsage(ctx, &gen.GetPeriodUsagePayload{})

	require.NoError(t, err)
	require.Equal(t, 7, result.ActualEnabledServerCount, "should use DB count, not cached value")
}
