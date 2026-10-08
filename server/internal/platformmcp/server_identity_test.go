package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/servernames"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

var (
	overviewChatServerID    = uuid.MustParse("0192a3b4-0000-7000-8000-000000000001")
	overviewBillingServerID = uuid.MustParse("0192a3b4-0000-7000-8000-000000000002")
)

func overviewResolver() *servernames.Resolver {
	return servernames.NewResolver(configuredServers([]platformrepo.ListPlatformMCPServerIdentitiesRow{
		{McpServerID: overviewChatServerID, McpName: "External Acme Chat", McpSlug: "acme-chat", ToolsetSlug: "", PluginSlug: "acme-tools", PluginDisplayName: "External Acme Chat"},
		{McpServerID: overviewBillingServerID, McpName: "Billing", McpSlug: "billing", ToolsetSlug: "billing-tools", PluginSlug: "finance", PluginDisplayName: "Billing"},
		{McpServerID: overviewBillingServerID, McpName: "Billing", McpSlug: "billing", ToolsetSlug: "billing-tools", PluginSlug: "support", PluginDisplayName: "Billing (Support)"},
	}))
}

// TestAttributeTopServers_FoldsReportedNamesOntoConfiguredServers pins the
// production shape: one configured server reached as a plugin-routed prefix, a
// bare slug, its display name, and its id is one entry carrying the configured
// id, and names nothing is configured under stay as the agent reported them.
func TestAttributeTopServers_FoldsReportedNamesOntoConfiguredServers(t *testing.T) {
	t.Parallel()

	servers := attributeTopServers([]telemetryrepo.TopServer{
		{ServerName: "plugin_acme-tools_External_Acme_Chat", ToolCallCount: 40},
		{ServerName: "claude-in-chrome", ToolCallCount: 30},
		{ServerName: "acme-chat", ToolCallCount: 25},
		{ServerName: "plugin_finance_Billing", ToolCallCount: 20},
		{ServerName: "/usr/local/bin/local-stdio-server", ToolCallCount: 12},
		{ServerName: "External Acme Chat", ToolCallCount: 10},
		{ServerName: "plugin_support_Billing_Support", ToolCallCount: 9},
		{ServerName: overviewChatServerID.String(), ToolCallCount: 5},
		{ServerName: "BILLING", ToolCallCount: 1},
	}, overviewResolver(), maxOverviewServers)

	require.Equal(t, []ProjectOverviewServer{
		{Name: "External Acme Chat", MCPID: overviewChatServerID.String(), ToolCalls: 80},
		{Name: "Billing", MCPID: overviewBillingServerID.String(), ToolCalls: 30},
		{Name: "claude-in-chrome", MCPID: "", ToolCalls: 30},
		{Name: "/usr/local/bin/local-stdio-server", MCPID: "", ToolCalls: 12},
	}, servers)
}

func TestAttributeTopServers_CapsAfterFolding(t *testing.T) {
	t.Parallel()

	servers := attributeTopServers([]telemetryrepo.TopServer{
		{ServerName: "unknown-a", ToolCallCount: 5},
		{ServerName: "acme-chat", ToolCallCount: 4},
		{ServerName: "External Acme Chat", ToolCallCount: 4},
		{ServerName: "unknown-b", ToolCallCount: 3},
	}, overviewResolver(), 2)

	require.Equal(t, []ProjectOverviewServer{
		{Name: "External Acme Chat", MCPID: overviewChatServerID.String(), ToolCalls: 8},
		{Name: "unknown-a", MCPID: "", ToolCalls: 5},
	}, servers)
}

func TestAttributeTopServers_WithoutConfiguredServersKeepsReportedNames(t *testing.T) {
	t.Parallel()

	servers := attributeTopServers([]telemetryrepo.TopServer{
		{ServerName: "linear", ToolCallCount: 3},
	}, servernames.NewResolver(nil), maxOverviewServers)
	require.Equal(t, []ProjectOverviewServer{{Name: "linear", MCPID: "", ToolCalls: 3}}, servers)

	require.Empty(t, attributeTopServers(nil, overviewResolver(), maxOverviewServers))
}

func TestConfiguredServers_FoldsMembershipRowsPerServer(t *testing.T) {
	t.Parallel()

	servers := configuredServers([]platformrepo.ListPlatformMCPServerIdentitiesRow{
		{McpServerID: overviewBillingServerID, McpName: "Billing", McpSlug: "billing", ToolsetSlug: "billing-tools", PluginSlug: "finance", PluginDisplayName: "Billing"},
		// A server distributed through no plugin arrives as one row with empty
		// plugin columns and gets no membership.
		{McpServerID: overviewChatServerID, McpName: "External Acme Chat", McpSlug: "acme-chat", ToolsetSlug: "", PluginSlug: "", PluginDisplayName: ""},
		{McpServerID: overviewBillingServerID, McpName: "Billing", McpSlug: "billing", ToolsetSlug: "billing-tools", PluginSlug: "support", PluginDisplayName: "Billing (Support)"},
	})

	require.Equal(t, []servernames.ConfiguredServer{
		{
			ID:          overviewBillingServerID.String(),
			Name:        "Billing",
			Slug:        "billing",
			ToolsetSlug: "billing-tools",
			Plugins: []servernames.PluginMembership{
				{PluginSlug: "finance", DisplayName: "Billing"},
				{PluginSlug: "support", DisplayName: "Billing (Support)"},
			},
		},
		{
			ID:          overviewChatServerID.String(),
			Name:        "External Acme Chat",
			Slug:        "acme-chat",
			ToolsetSlug: "",
			Plugins:     nil,
		},
	}, servers)
}

// TestOverviewObserved_NeverReportsNoObservationsBesideANonzeroMetric pins the
// same consistency rule for the project overview, whose hook-observed servers
// are not counted by the gateway summary.
func TestOverviewObserved_NeverReportsNoObservationsBesideANonzeroMetric(t *testing.T) {
	t.Parallel()

	require.False(t, overviewObserved(nil, 0, nil, 0))
	require.False(t, overviewObserved(&telemetryrepo.OverviewSummary{}, 0, []ProjectOverviewServer{}, 0))

	require.True(t, overviewObserved(&telemetryrepo.OverviewSummary{TotalToolCalls: 1}, 0, nil, 0))
	// The exact unified count can observe attributed servers even when the
	// bounded top-server presentation list is empty.
	require.True(t, overviewObserved(nil, 2, nil, 0))
	require.True(t, overviewObserved(nil, 0, nil, 1))
	require.True(t, overviewObserved(nil, 0, []ProjectOverviewServer{{Name: "linear", MCPID: "", ToolCalls: 3}}, 0))
	// Session mode: chat participants with no tool calls anywhere are still an
	// observation, because active_users reports them.
	require.True(t, overviewObserved(&telemetryrepo.OverviewSummary{}, 0, nil, 4))
}

// TestReconcileMetrics_NeverReportsNoObservationsBesideANonzeroMetric pins the
// consistency rule: a window the gateway summary observed is observed, whatever
// the call-level tally found.
func TestReconcileMetrics_NeverReportsNoObservationsBesideANonzeroMetric(t *testing.T) {
	t.Parallel()

	// Latency alone proves the gateway saw calls.
	metrics := reconcileMetrics(outcomeTotals{}, &telemetryrepo.OverviewSummary{AvgLatencyMs: 385})
	require.True(t, metrics.observed)
	require.InDelta(t, 385, metrics.avgLatencyMs, 0)

	// A count alone does too.
	metrics = reconcileMetrics(outcomeTotals{}, &telemetryrepo.OverviewSummary{TotalToolCalls: 3})
	require.True(t, metrics.observed)

	// And so does the tally alone.
	metrics = reconcileMetrics(outcomeTotals{Total: 2, Success: 2}, &telemetryrepo.OverviewSummary{})
	require.True(t, metrics.observed)

	// Nothing on either side is genuinely unobserved.
	metrics = reconcileMetrics(outcomeTotals{}, &telemetryrepo.OverviewSummary{})
	require.False(t, metrics.observed)
	metrics = reconcileMetrics(outcomeTotals{}, nil)
	require.False(t, metrics.observed)
}

// TestReconcileMetrics_UsesGatewayCountsWhenTheTallySawNothing pins the
// fallback: gateway telemetry that counted calls is reported rather than a
// zero that the latency in the same payload would contradict.
func TestReconcileMetrics_UsesGatewayCountsWhenTheTallySawNothing(t *testing.T) {
	t.Parallel()

	metrics := reconcileMetrics(outcomeTotals{}, &telemetryrepo.OverviewSummary{
		TotalToolCalls:  12,
		FailedToolCalls: 3,
		AvgLatencyMs:    7105,
	})
	require.Equal(t, int64(12), metrics.toolCalls)
	require.Equal(t, int64(3), metrics.failedToolCalls)
	require.InDelta(t, 7105, metrics.avgLatencyMs, 0)
	require.True(t, metrics.observed)
}

// TestReconcileMetrics_PrefersTheClassifiedTally pins that the call-level tally
// leads whenever it saw anything, because it is what classifies outcomes.
func TestReconcileMetrics_PrefersTheClassifiedTally(t *testing.T) {
	t.Parallel()

	metrics := reconcileMetrics(outcomeTotals{Total: 5, Success: 3, ServerError: 2}, &telemetryrepo.OverviewSummary{
		TotalToolCalls:  9,
		FailedToolCalls: 1,
		AvgLatencyMs:    420,
	})
	require.Equal(t, int64(5), metrics.toolCalls)
	require.Equal(t, int64(2), metrics.failedToolCalls)
	require.InDelta(t, 420, metrics.avgLatencyMs, 0)
	require.True(t, metrics.observed)
}
