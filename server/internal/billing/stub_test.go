package billing_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newStub(t *testing.T) *billing.StubClient {
	t.Helper()

	// The stub writes period/usage data to ./scratch/ in the working dir.
	// Redirect to a temp dir so tests can't see each other's files.
	t.Chdir(t.TempDir())

	return billing.NewStubClient(testenv.NewLogger(t), testenv.NewTracerProvider(t))
}

func TestStub_GetCustomerTier_ReturnsPro(t *testing.T) {
	stub := newStub(t)

	tier, ok, err := stub.GetCustomerTier(t.Context(), "org-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, tier)
	require.Equal(t, billing.TierPro, *tier)
}

func TestStub_GetUsageTiers_ContainsAllTiers(t *testing.T) {
	stub := newStub(t)

	tiers, err := stub.GetUsageTiers(t.Context())
	require.NoError(t, err)
	require.NotNil(t, tiers)
	require.NotNil(t, tiers.Free)
	require.NotNil(t, tiers.Pro)
	require.NotNil(t, tiers.Enterprise)

	// Free tier is gratis.
	require.Equal(t, float64(0), tiers.Free.BasePrice)
	require.GreaterOrEqual(t, tiers.Pro.BasePrice, float64(0))

	// Pro must include more tool calls and servers than Free.
	require.Greater(t, tiers.Pro.IncludedToolCalls, tiers.Free.IncludedToolCalls)
	require.Greater(t, tiers.Pro.IncludedServers, tiers.Free.IncludedServers)
}

func TestStub_GetCustomer_ZeroUsageWhenNoFile(t *testing.T) {
	stub := newStub(t)

	cust, err := stub.GetCustomer(t.Context(), "org-zero")
	require.NoError(t, err)
	require.Equal(t, "org-zero", cust.OrganizationID)
	require.NotNil(t, cust.PeriodUsage)
	require.EqualValues(t, 0, cust.PeriodUsage.ToolCalls)
	require.EqualValues(t, 0, cust.PeriodUsage.Servers)
}

func TestStub_TrackToolCallUsage_IncrementsCounter(t *testing.T) {
	stub := newStub(t)
	ctx := t.Context()

	stub.TrackToolCallUsage(ctx, billing.ToolCallUsageEvent{
		OrganizationID: "org-tool",
		Type:           billing.ToolCallTypeHTTP,
	})
	stub.TrackToolCallUsage(ctx, billing.ToolCallUsageEvent{
		OrganizationID: "org-tool",
		Type:           billing.ToolCallTypeFunction,
	})

	pu, err := stub.GetPeriodUsage(ctx, "org-tool")
	require.NoError(t, err)
	require.EqualValues(t, 2, pu.ToolCalls)
}

func TestStub_TrackPlatformUsage_UpdatesServerCounts(t *testing.T) {
	stub := newStub(t)
	ctx := t.Context()

	stub.TrackPlatformUsage(ctx, []billing.PlatformUsageEvent{
		{OrganizationID: "org-p", PrivateMCPServers: 4, PublicMCPServers: 7},
	})

	pu, err := stub.GetPeriodUsage(ctx, "org-p")
	require.NoError(t, err)
	require.EqualValues(t, 4, pu.Servers)
	require.EqualValues(t, 7, pu.ActualEnabledServerCount)
}

func TestStub_TrackPlatformUsage_PerOrgIsolated(t *testing.T) {
	stub := newStub(t)
	ctx := t.Context()

	stub.TrackPlatformUsage(ctx, []billing.PlatformUsageEvent{
		{OrganizationID: "org-A", PrivateMCPServers: 1},
		{OrganizationID: "org-B", PrivateMCPServers: 9},
	})

	a, err := stub.GetPeriodUsage(ctx, "org-A")
	require.NoError(t, err)
	require.EqualValues(t, 1, a.Servers)

	b, err := stub.GetPeriodUsage(ctx, "org-B")
	require.NoError(t, err)
	require.EqualValues(t, 9, b.Servers)
}

func TestStub_TrackModelUsage_PersistedAccumulation(t *testing.T) {
	stub := newStub(t)
	ctx := t.Context()

	cost1 := 0.001
	cost2 := 0.002
	stub.TrackModelUsage(ctx, billing.ModelUsageEvent{
		OrganizationID: "org-m",
		InputTokens:    100,
		OutputTokens:   50,
		TotalTokens:    150,
		Cost:           &cost1,
	})
	stub.TrackModelUsage(ctx, billing.ModelUsageEvent{
		OrganizationID: "org-m",
		InputTokens:    10,
		OutputTokens:   5,
		TotalTokens:    15,
		Cost:           &cost2,
	})

	// Stub persists model usage but only exposes period usage publicly. We
	// at least confirm subsequent reads don't panic and tool-call counter
	// is unaffected.
	pu, err := stub.GetPeriodUsage(ctx, "org-m")
	require.NoError(t, err)
	require.EqualValues(t, 0, pu.ToolCalls)
}

func TestStub_GetStoredPeriodUsage_MatchesGetPeriodUsage(t *testing.T) {
	stub := newStub(t)
	ctx := t.Context()

	stub.TrackToolCallUsage(ctx, billing.ToolCallUsageEvent{OrganizationID: "org-stored"})

	stored, err := stub.GetStoredPeriodUsage(ctx, "org-stored")
	require.NoError(t, err)
	live, err := stub.GetPeriodUsage(ctx, "org-stored")
	require.NoError(t, err)
	require.Equal(t, stored.ToolCalls, live.ToolCalls)
}

func TestStub_CreateCheckout_NotImplemented(t *testing.T) {
	stub := newStub(t)

	url, err := stub.CreateCheckout(t.Context(), "org", "https://server", "https://success")
	require.Error(t, err)
	require.Empty(t, url)
}

func TestStub_CreateCustomerSession_NotImplemented(t *testing.T) {
	stub := newStub(t)

	url, err := stub.CreateCustomerSession(t.Context(), "org")
	require.Error(t, err)
	require.Empty(t, url)
}

func TestStub_ValidateAndParseWebhookEvent_NotImplemented(t *testing.T) {
	stub := newStub(t)

	got, err := stub.ValidateAndParseWebhookEvent(t.Context(), []byte(`{}`), http.Header{})
	require.Error(t, err)
	require.Nil(t, got)
}

func TestStub_InvalidateBillingCustomerCaches_NoError(t *testing.T) {
	stub := newStub(t)
	require.NoError(t, stub.InvalidateBillingCustomerCaches(t.Context(), "org"))
}
