package aiintegrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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

func TestBuildClaudeChatMetricRowsEmitsUsageAndCostRows(t *testing.T) {
	t.Parallel()

	cfg := testAnalyticsConfig()

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
	}
	costRows := []anthropicapi.UserCostRow{
		// Spend for the same bucket as the usage row: emitted as its own
		// cost row, not merged.
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
		// Deleted user (null email) still yields a cost row.
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

	usageEvents, err := buildClaudeChatUsageRows(cfg, usageRows)
	require.NoError(t, err)
	costEvents, err := buildClaudeChatCostRows(cfg, costRows)
	require.NoError(t, err)
	events := slices.Concat(usageEvents, costEvents)
	require.Len(t, events, 3, "one usage row plus two cost rows")

	usage := events[0]
	require.Equal(t, claudeChatUsageMetricsURN, usage.ToolInfo.URN)
	require.Equal(t, time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), usage.Timestamp)
	require.Equal(t, "dev@example.com", usage.UserInfo.Email())
	require.Equal(t, int64(100), usage.Attributes[attr.GenAIUsageInputTokensKey])
	require.Equal(t, int64(50), usage.Attributes[attr.GenAIUsageOutputTokensKey])
	require.Equal(t, int64(3200), usage.Attributes[attr.GenAIUsageCacheReadInputTokensKey])
	require.Equal(t, int64(1500), usage.Attributes[attr.GenAIUsageCacheCreationInputTokensKey])
	require.NotContains(t, usage.Attributes, attr.GenAIUsageCostKey, "usage rows carry no cost")
	require.Equal(t, "claude-opus-4-8", usage.Attributes[attr.GenAIResponseModelKey])
	require.Equal(t, generateClaudeChatUsageRowHash(usageRows[0]), usage.Attributes[attr.ClaudeChatEventHashKey])

	cost := events[1]
	require.Equal(t, claudeChatCostMetricsURN, cost.ToolInfo.URN)
	require.Equal(t, time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), cost.Timestamp)
	require.InDelta(t, 1.50, cost.Attributes[attr.GenAIUsageCostKey], 0.0001)
	require.NotContains(t, cost.Attributes, attr.GenAIUsageInputTokensKey, "cost rows carry no tokens")
	require.Equal(t, generateClaudeChatCostRowHash(costRows[0]), cost.Attributes[attr.ClaudeChatEventHashKey])

	deletedUserCost := events[2]
	require.Equal(t, claudeChatCostMetricsURN, deletedUserCost.ToolInfo.URN)
	require.Equal(t, time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC), deletedUserCost.Timestamp)
	require.Empty(t, deletedUserCost.UserInfo.Email())
	require.InDelta(t, 0.42, deletedUserCost.Attributes[attr.GenAIUsageCostKey], 0.0001)
	require.Equal(t, "user_2", deletedUserCost.Attributes[attr.ExternalUserIDKey])

	// Every row carries a distinct hash; re-building the same rows yields
	// identical hashes so re-ingested windows can be deduped.
	hashes := map[any]bool{}
	for _, event := range events {
		hashes[event.Attributes[attr.ClaudeChatEventHashKey]] = true
	}
	require.Len(t, hashes, 3, "hashes are distinct across rows")

	rebuiltUsage, err := buildClaudeChatUsageRows(cfg, usageRows)
	require.NoError(t, err)
	rebuiltCost, err := buildClaudeChatCostRows(cfg, costRows)
	require.NoError(t, err)
	rebuilt := slices.Concat(rebuiltUsage, rebuiltCost)
	for i := range events {
		require.Equal(t, events[i].Attributes[attr.ClaudeChatEventHashKey], rebuilt[i].Attributes[attr.ClaudeChatEventHashKey])
	}
}

func TestClaudeChatRowHashesDistinguishKindAndContent(t *testing.T) {
	t.Parallel()

	usageRow := anthropicapi.UserUsageRow{
		Actor:                anthropicapi.AnalyticsActor{UserID: "user_1", Email: nil, Name: nil, Deleted: false},
		StartingAt:           "2026-07-16T10:00:00Z",
		EndingAt:             "2026-07-16T10:01:00Z",
		Model:                "claude-opus-4-8",
		Product:              "chat",
		UncachedInputTokens:  100,
		OutputTokens:         50,
		CacheReadInputTokens: 0,
		CacheCreation:        anthropicapi.AnalyticsCacheCreation{Ephemeral1hInputTokens: 0, Ephemeral5mInputTokens: 0},
		TotalTokens:          150,
		Requests:             1,
	}
	costRow := anthropicapi.UserCostRow{
		Actor:      anthropicapi.AnalyticsActor{UserID: "user_1", Email: nil, Name: nil, Deleted: false},
		StartingAt: "2026-07-16T10:00:00Z",
		EndingAt:   "2026-07-16T10:01:00Z",
		Model:      "claude-opus-4-8",
		Product:    "chat",
		Amount:     "100.000000",
		ListAmount: "100.000000",
		Currency:   "USD",
		Requests:   1,
	}

	// The same aggregation key hashes differently per report kind.
	require.NotEqual(t, generateClaudeChatUsageRowHash(usageRow), generateClaudeChatCostRowHash(costRow))

	changed := usageRow
	changed.OutputTokens = 51
	require.NotEqual(t, generateClaudeChatUsageRowHash(usageRow), generateClaudeChatUsageRowHash(changed))
}

func TestNewClaudeChatLogParamsStampsProvenance(t *testing.T) {
	t.Parallel()

	cfg := testAnalyticsConfig()
	actor := anthropicapi.AnalyticsActor{UserID: "user_1", Email: new("Dev@Example.com"), Name: new("Dev"), Deleted: false}

	params := newClaudeChatLogParams(cfg, claudeChatUsageMetricsURN, "Claude Chat usage metrics", time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), actor, "claude-opus-4-8")

	require.Equal(t, claudeChatUsageMetricsURN, params.ToolInfo.URN)
	require.Equal(t, cfg.OrganizationID, params.ToolInfo.OrganizationID)
	require.Equal(t, cfg.ProjectID.String(), params.ToolInfo.ProjectID)
	require.Equal(t, "dev@example.com", params.UserInfo.Email())
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
	pollers := newTestAnalyticsPollers(t, store, api.server.URL)

	cfg := createAnthropicComplianceConfig(t, ctx, conn, store, orgID)

	pollers.syncBoth(t, ctx, cfg, endTime)

	// One finality probe plus one window fetch per endpoint.
	require.Equal(t, 2, api.usageRequests())
	require.Equal(t, 2, api.costRequests())

	// The probe is a minimal single-bucket request.
	probe := api.usageQueries()[0]
	require.Equal(t, "1", probe.Get("limit"))

	// The window fetch must cover exactly the 24h initial lookback.
	window := api.usageQueries()[1]
	require.Equal(t, endTime.Add(-24*time.Hour).Truncate(time.Minute).Format(time.RFC3339), window.Get("starting_at"))
	require.Equal(t, endTime.Truncate(time.Minute).Format(time.RFC3339), window.Get("ending_at"))
	require.Equal(t, "1m", window.Get("bucket_width"))
	require.Equal(t, []string{"chat"}, window["products[]"])

	// Both schedules advance independently to the same point.
	for _, schedule := range []string{ScheduleAnthropicAnalyticsUsage, ScheduleAnthropicAnalyticsCost} {
		state, err := store.EnsureTimeSyncSchedule(ctx, cfg.ID, schedule)
		require.NoError(t, err)
		require.Equal(t, endTime.Truncate(time.Minute), state.WatermarkAt.UTC())
		require.Empty(t, state.LastPollError)
	}
}

func TestMaybeSyncAnthropicAnalyticsChunksBacklogIntoWindows(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	endTime := time.Now().UTC().Truncate(time.Second)
	api := newFakeAnalyticsAPI(t, endTime)
	pollers := newTestAnalyticsPollers(t, store, api.server.URL)

	cfg := createAnthropicComplianceConfig(t, ctx, conn, store, orgID)

	// Seed a usage watermark 3 days back to simulate an outage backlog. The
	// cost schedule stays fresh, so it only does its initial lookback.
	_, err := store.EnsureTimeSyncSchedule(ctx, cfg.ID, ScheduleAnthropicAnalyticsUsage)
	require.NoError(t, err)
	backlogStart := endTime.Add(-72 * time.Hour).Truncate(time.Minute)
	require.NoError(t, store.AdvanceSchedulePollWatermark(ctx, cfg.ID, ScheduleAnthropicAnalyticsUsage, backlogStart))

	pollers.syncBoth(t, ctx, cfg, endTime)

	// 72h of backlog at a 24h-per-request cap is 3 usage windows, plus the
	// finality probe. The independent cost schedule does probe + one window.
	require.Equal(t, 4, api.usageRequests())
	require.Equal(t, 2, api.costRequests())
	queries := api.usageQueries()
	require.Equal(t, backlogStart.Format(time.RFC3339), queries[1].Get("starting_at"))
	require.Equal(t, backlogStart.Add(24*time.Hour).Format(time.RFC3339), queries[1].Get("ending_at"))
	require.Equal(t, backlogStart.Add(24*time.Hour).Format(time.RFC3339), queries[2].Get("starting_at"))

	state, err := store.EnsureTimeSyncSchedule(ctx, cfg.ID, ScheduleAnthropicAnalyticsUsage)
	require.NoError(t, err)
	require.Equal(t, endTime.Truncate(time.Minute), state.WatermarkAt.UTC())
}

func TestMaybeSyncAnthropicAnalyticsPullsOnlyUpToDataRefreshedAt(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	endTime := time.Now().UTC().Truncate(time.Second)
	// The export watermark is 6 hours behind: it caps the pull, so the window
	// request must end at data_refreshed_at and the stored watermark must
	// stop there.
	refreshedAt := endTime.Add(-6 * time.Hour)
	api := newFakeAnalyticsAPI(t, refreshedAt)
	pollers := newTestAnalyticsPollers(t, store, api.server.URL)

	cfg := createAnthropicComplianceConfig(t, ctx, conn, store, orgID)

	pollers.syncBoth(t, ctx, cfg, endTime)

	require.Equal(t, 2, api.usageRequests(), "one probe plus one window")
	window := api.usageQueries()[1]
	require.Equal(t, endTime.Add(-24*time.Hour).Truncate(time.Minute).Format(time.RFC3339), window.Get("starting_at"))
	require.Equal(t, refreshedAt.Truncate(time.Minute).Format(time.RFC3339), window.Get("ending_at"))

	state, err := store.EnsureTimeSyncSchedule(ctx, cfg.ID, ScheduleAnthropicAnalyticsUsage)
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
	pollers := newTestAnalyticsPollers(t, store, server.URL)

	cfg := createAnthropicComplianceConfig(t, ctx, conn, store, orgID)

	usageCfg, err := store.GetUsagePollConfig(ctx, cfg.ID, ScheduleAnthropicAnalyticsUsage)
	require.NoError(t, err)
	costCfg, err := store.GetUsagePollConfig(ctx, cfg.ID, ScheduleAnthropicAnalyticsCost)
	require.NoError(t, err)
	require.ErrorContains(t, pollers.usage.Sync(ctx, usageCfg, endTime), "403 Forbidden")
	require.ErrorContains(t, pollers.cost.Sync(ctx, costCfg, endTime), "403 Forbidden")
}

func TestMaybeSyncAnthropicAnalyticsCostFailureDoesNotBlockUsage(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	endTime := time.Now().UTC().Truncate(time.Second)

	// Usage responds normally; cost is denied.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/organizations/analytics/user_cost_report" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":              []any{},
			"data_refreshed_at": endTime.Format(time.RFC3339),
			"has_more":          false,
			"next_page":         "",
			"organization_id":   "org_ext_1",
		})
	}))
	t.Cleanup(server.Close)

	pollers := newTestAnalyticsPollers(t, store, server.URL)
	cfg := createAnthropicComplianceConfig(t, ctx, conn, store, orgID)

	usageCfg, err := store.GetUsagePollConfig(ctx, cfg.ID, ScheduleAnthropicAnalyticsUsage)
	require.NoError(t, err)
	costCfg, err := store.GetUsagePollConfig(ctx, cfg.ID, ScheduleAnthropicAnalyticsCost)
	require.NoError(t, err)
	require.NoError(t, pollers.usage.Sync(ctx, usageCfg, endTime))
	require.ErrorContains(t, pollers.cost.Sync(ctx, costCfg, endTime), "403 Forbidden")

	usageState, err := store.EnsureTimeSyncSchedule(ctx, cfg.ID, ScheduleAnthropicAnalyticsUsage)
	require.NoError(t, err)
	require.Equal(t, endTime.Truncate(time.Minute), usageState.WatermarkAt.UTC())
	require.Empty(t, usageState.LastPollError)

	costState, err := store.EnsureTimeSyncSchedule(ctx, cfg.ID, ScheduleAnthropicAnalyticsCost)
	require.NoError(t, err)
	require.True(t, costState.WatermarkAt.IsZero())
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

// testAnalyticsPollers holds the two per-report poller services, mirroring
// how the poll activity wires them.
type testAnalyticsPollers struct {
	usage *AnthropicAnalyticsPoller
	cost  *AnthropicAnalyticsPoller
	store *Store
}

func (p testAnalyticsPollers) syncBoth(t *testing.T, ctx context.Context, cfg Config, endTime time.Time) {
	t.Helper()
	usageCfg, err := p.store.GetUsagePollConfig(ctx, cfg.ID, ScheduleAnthropicAnalyticsUsage)
	require.NoError(t, err)
	costCfg, err := p.store.GetUsagePollConfig(ctx, cfg.ID, ScheduleAnthropicAnalyticsCost)
	require.NoError(t, err)
	require.NoError(t, p.usage.Sync(ctx, usageCfg, endTime))
	require.NoError(t, p.cost.Sync(ctx, costCfg, endTime))
}

func newTestAnalyticsPollers(t *testing.T, store *Store, baseURL string) testAnalyticsPollers {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	telemetryLogger := telemetry.NewStub(logger)
	heartbeat := func(ctx context.Context, scope string, page int) {}

	usage := NewAnthropicUsageAnalyticsPoller(store, policy, telemetryLogger, heartbeat)
	usage.baseURL = baseURL
	cost := NewAnthropicCostAnalyticsPoller(store, policy, telemetryLogger, heartbeat)
	cost.baseURL = baseURL
	return testAnalyticsPollers{usage: usage, cost: cost, store: store}
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
			api.mu.Unlock()
			return
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
