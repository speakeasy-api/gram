package aiintegrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
)

func TestBuildAnthropicUsageEventsJoinsUsageAndCost(t *testing.T) {
	t.Parallel()

	cfg := testAnalyticsConfig()
	cutoff := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	usageRows := []anthropicapi.UserUsageRow{
		{
			Actor:                anthropicapi.AnalyticsActor{UserID: "user_1", Email: new("Dev@Example.com"), Name: new("Dev"), Deleted: false},
			StartingAt:           "2026-07-16T10:00:00Z",
			EndingAt:             "2026-07-16T10:01:00Z",
			Model:                "claude-opus-4-8",
			Product:              "",
			UncachedInputTokens:  100,
			OutputTokens:         50,
			CacheReadInputTokens: 3200,
			CacheCreation:        anthropicapi.AnalyticsCacheCreation{Ephemeral1hInputTokens: 1000, Ephemeral5mInputTokens: 500},
			TotalTokens:          4850,
			Requests:             2,
		},
		// Bucket at the cutoff: incomplete, must be dropped.
		{
			Actor:                anthropicapi.AnalyticsActor{UserID: "user_1", Email: new("dev@example.com"), Name: new("Dev"), Deleted: false},
			StartingAt:           "2026-07-16T12:00:00Z",
			EndingAt:             "2026-07-16T12:01:00Z",
			Model:                "claude-opus-4-8",
			Product:              "",
			UncachedInputTokens:  999,
			OutputTokens:         999,
			CacheReadInputTokens: 0,
			CacheCreation:        anthropicapi.AnalyticsCacheCreation{Ephemeral1hInputTokens: 0, Ephemeral5mInputTokens: 0},
			TotalTokens:          1998,
			Requests:             1,
		},
	}
	costRows := []anthropicapi.UserCostRow{
		// Matches the first usage row.
		{
			Actor:      anthropicapi.AnalyticsActor{UserID: "user_1", Email: new("dev@example.com"), Name: new("Dev"), Deleted: false},
			StartingAt: "2026-07-16T10:00:00Z",
			EndingAt:   "2026-07-16T10:01:00Z",
			Model:      "claude-opus-4-8",
			Product:    "",
			Amount:     "150.000000",
			ListAmount: "150.000000",
			Currency:   "USD",
			Requests:   2,
		},
		// Cost-only row (no usage counterpart) still yields an event.
		{
			Actor:      anthropicapi.AnalyticsActor{UserID: "user_2", Email: nil, Name: nil, Deleted: true},
			StartingAt: "2026-07-16T11:00:00Z",
			EndingAt:   "2026-07-16T11:01:00Z",
			Model:      "claude-sonnet-5",
			Product:    "",
			Amount:     "42.000000",
			ListAmount: "42.000000",
			Currency:   "USD",
			Requests:   1,
		},
	}

	events, err := buildAnthropicUsageEvents(cfg, usageRows, costRows, cutoff)
	require.NoError(t, err)
	require.Len(t, events, 2)

	joined := events[0]
	require.Equal(t, time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), joined.Timestamp)
	require.Equal(t, "dev@example.com", joined.UserInfo.Email())
	require.Equal(t, int64(100), joined.Attributes[attr.GenAIUsageInputTokensKey])
	require.Equal(t, int64(50), joined.Attributes[attr.GenAIUsageOutputTokensKey])
	require.Equal(t, int64(3200), joined.Attributes[attr.GenAIUsageCacheReadInputTokensKey])
	require.Equal(t, int64(1500), joined.Attributes[attr.GenAIUsageCacheCreationInputTokensKey])
	require.InDelta(t, 1.50, joined.Attributes[attr.GenAIUsageCostKey], 0.0001)
	require.Equal(t, "claude-opus-4-8", joined.Attributes[attr.GenAIResponseModelKey])

	costOnly := events[1]
	require.Equal(t, time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC), costOnly.Timestamp)
	require.Empty(t, costOnly.UserInfo.Email())
	require.Equal(t, int64(0), costOnly.Attributes[attr.GenAIUsageInputTokensKey])
	require.InDelta(t, 0.42, costOnly.Attributes[attr.GenAIUsageCostKey], 0.0001)
	require.Equal(t, "user_2", costOnly.Attributes[attr.ExternalUserIDKey])
}

func TestBuildAnthropicUsageLogParamsStampsProvenance(t *testing.T) {
	t.Parallel()

	cfg := testAnalyticsConfig()
	event := &anthropicUsageEvent{
		bucketStart:          time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		email:                "dev@example.com",
		externalUserID:       "user_1",
		model:                "claude-opus-4-8",
		uncachedInputTokens:  100,
		outputTokens:         50,
		cacheReadInputTokens: 3200,
		cacheCreationTokens:  1500,
		costUSD:              1.5,
	}

	params := buildAnthropicUsageLogParams(cfg, event)

	require.Equal(t, anthropicUsageMetricsURN, params.ToolInfo.URN)
	require.Equal(t, cfg.OrganizationID, params.ToolInfo.OrganizationID)
	require.Equal(t, cfg.ProjectID.String(), params.ToolInfo.ProjectID)
	require.Equal(t, "claude-chat", params.Attributes[attr.HookSourceKey])
	require.Equal(t, cfg.ID.String(), params.Attributes[attr.AIIntegrationConfigIDKey])
	require.Equal(t, "anthropic", params.Attributes[attr.ProviderKey])
	require.Equal(t, "team", params.Attributes[attr.AccountTypeKey])
	require.Equal(t, "org_ext_1", params.Attributes[attr.ExternalOrgIDKey])
	require.Equal(t, "flat_rate", params.Attributes[attr.BillingModeKey])
	require.Equal(t, string(telemetry.EventSourceAPI), params.Attributes[attr.EventSourceKey])
	require.Equal(t, "user_1", params.Attributes[attr.ExternalUserIDKey])
}

func TestMaybeSyncAnthropicAnalyticsFirstSyncAdvancesWatermark(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	endTime := time.Now().UTC().Truncate(time.Second)
	api := newFakeAnalyticsAPI(t, endTime)
	svc := newTestAnalyticsPollService(t, store, api.server.URL)

	cfg := createAnthropicComplianceConfig(t, ctx, conn, store, orgID)

	svc.MaybeSyncAnthropicAnalytics(ctx, cfg, endTime)

	require.Equal(t, 1, api.usageRequests())
	require.Equal(t, 1, api.costRequests())

	// The first request must cover exactly the 24h initial lookback.
	first := api.usageQueries()[0]
	require.Equal(t, endTime.Add(-24*time.Hour).Truncate(time.Minute).Format(time.RFC3339), first.Get("starting_at"))
	require.Equal(t, endTime.Truncate(time.Minute).Format(time.RFC3339), first.Get("ending_at"))
	require.Equal(t, "1m", first.Get("bucket_width"))
	require.Equal(t, []string{"chat"}, first["products[]"])

	state, err := store.EnsureAnalyticsSync(ctx, cfg.ID)
	require.NoError(t, err)
	require.Equal(t, endTime.Truncate(time.Minute), state.WatermarkAt.UTC())
	require.Empty(t, state.LastPollError)
	require.Equal(t, endTime.Add(anthropicAnalyticsPollInterval), state.NextPollAfter.UTC())

	// Not due yet: a second invocation at the same time must not hit the API.
	svc.MaybeSyncAnthropicAnalytics(ctx, cfg, endTime)
	require.Equal(t, 1, api.usageRequests())
}

func TestMaybeSyncAnthropicAnalyticsChunksBacklogIntoWindows(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	endTime := time.Now().UTC().Truncate(time.Second)
	api := newFakeAnalyticsAPI(t, endTime)
	svc := newTestAnalyticsPollService(t, store, api.server.URL)

	cfg := createAnthropicComplianceConfig(t, ctx, conn, store, orgID)

	// Seed a watermark 3 days back to simulate an outage backlog.
	_, err := store.EnsureAnalyticsSync(ctx, cfg.ID)
	require.NoError(t, err)
	backlogStart := endTime.Add(-72 * time.Hour).Truncate(time.Minute)
	require.NoError(t, store.AdvanceAnalyticsPollWatermark(ctx, cfg.ID, backlogStart))

	svc.MaybeSyncAnthropicAnalytics(ctx, cfg, endTime)

	// 72h of backlog at a 24h-per-request cap is 3 usage windows.
	require.Equal(t, 3, api.usageRequests())
	queries := api.usageQueries()
	require.Equal(t, backlogStart.Format(time.RFC3339), queries[0].Get("starting_at"))
	require.Equal(t, backlogStart.Add(24*time.Hour).Format(time.RFC3339), queries[0].Get("ending_at"))
	require.Equal(t, backlogStart.Add(24*time.Hour).Format(time.RFC3339), queries[1].Get("starting_at"))

	state, err := store.EnsureAnalyticsSync(ctx, cfg.ID)
	require.NoError(t, err)
	require.Equal(t, endTime.Truncate(time.Minute), state.WatermarkAt.UTC())
}

func TestMaybeSyncAnthropicAnalyticsHoldsBackUnrefreshedBuckets(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	endTime := time.Now().UTC().Truncate(time.Second)
	// The export watermark is 6 hours behind: only buckets before it may be
	// ingested and the stored watermark must stop there.
	refreshedAt := endTime.Add(-6 * time.Hour)
	api := newFakeAnalyticsAPI(t, refreshedAt)
	svc := newTestAnalyticsPollService(t, store, api.server.URL)

	cfg := createAnthropicComplianceConfig(t, ctx, conn, store, orgID)

	svc.MaybeSyncAnthropicAnalytics(ctx, cfg, endTime)

	state, err := store.EnsureAnalyticsSync(ctx, cfg.ID)
	require.NoError(t, err)
	require.Equal(t, refreshedAt.Truncate(time.Minute), state.WatermarkAt.UTC())
	require.Empty(t, state.LastPollError)
}

func TestMaybeSyncAnthropicAnalyticsRecordsForbiddenAsFailure(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	endTime := time.Now().UTC().Truncate(time.Second)
	svc := newTestAnalyticsPollService(t, store, server.URL)

	cfg := createAnthropicComplianceConfig(t, ctx, conn, store, orgID)

	svc.MaybeSyncAnthropicAnalytics(ctx, cfg, endTime)

	state, err := store.EnsureAnalyticsSync(ctx, cfg.ID)
	require.NoError(t, err)
	require.True(t, state.WatermarkAt.IsZero())
	require.Contains(t, state.LastPollError, "read:analytics")
	require.Equal(t, int32(1), state.ConsecutiveFailures)
	require.Equal(t, endTime.Add(anthropicAnalyticsPollInterval), state.NextPollAfter.UTC())
}

func testAnalyticsConfig() Config {
	extOrgID := "org_ext_1"
	cfg := emptyConfig("org_123", ProviderAnthropicCompliance)
	cfg.ID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	cfg.ProjectID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	cfg.ExternalOrganizationID = &extOrgID
	cfg.BillingMode = "flat_rate"
	return cfg
}

func newTestAnalyticsPollService(t *testing.T, store *Store, baseURL string) *AnalyticsPollService {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	svc := NewAnalyticsPollService(
		testenv.NewLogger(t),
		store,
		policy,
		telemetry.NewStub(testenv.NewLogger(t)),
		func(ctx context.Context, scope string, page int) {},
	)
	svc.baseURL = baseURL
	return svc
}

func createAnthropicComplianceConfig(t *testing.T, ctx context.Context, conn *pgxpool.Pool, store *Store, orgID string) Config {
	t.Helper()

	extOrgID := "org_ext_1"
	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	result := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, &watermark)
	return result.Config
}

// fakeAnalyticsAPI serves empty usage/cost report pages with a fixed
// data_refreshed_at and records every request's query parameters.
type fakeAnalyticsAPI struct {
	server *httptest.Server

	mu         sync.Mutex
	usageCalls []url.Values
	costCalls  []url.Values
}

func newFakeAnalyticsAPI(t *testing.T, dataRefreshedAt time.Time) *fakeAnalyticsAPI {
	t.Helper()

	api := &fakeAnalyticsAPI{
		server:     nil,
		mu:         sync.Mutex{},
		usageCalls: nil,
		costCalls:  nil,
	}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		switch r.URL.Path {
		case "/v1/organizations/analytics/user_usage_report":
			api.usageCalls = append(api.usageCalls, r.URL.Query())
		case "/v1/organizations/analytics/user_cost_report":
			api.costCalls = append(api.costCalls, r.URL.Query())
		default:
			t.Errorf("unexpected analytics path %s", r.URL.Path)
		}
		api.mu.Unlock()

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":              []any{},
			"data_refreshed_at": dataRefreshedAt.UTC().Format(time.RFC3339),
			"has_more":          false,
			"next_page":         "",
			"organization_id":   "org_ext_1",
		})
	}))
	t.Cleanup(api.server.Close)
	return api
}

func (f *fakeAnalyticsAPI) usageRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.usageCalls)
}

func (f *fakeAnalyticsAPI) costRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.costCalls)
}

func (f *fakeAnalyticsAPI) usageQueries() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]url.Values, len(f.usageCalls))
	copy(out, f.usageCalls)
	return out
}
