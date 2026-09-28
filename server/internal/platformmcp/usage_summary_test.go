package platformmcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/servernames"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// usageRow is a per-target row as the usage pipeline classifies it.
func usageRow(targetType, targetID, label string, calls, failures uint64) telemetryrepo.ToolUsageTargetSummaryRow {
	return telemetryrepo.ToolUsageTargetSummaryRow{
		TargetType:   targetType,
		TargetKind:   "server",
		TargetID:     targetID,
		TargetLabel:  label,
		EventCount:   calls,
		UniqueTools:  1,
		SuccessCount: calls - failures,
		FailureCount: failures,
		FailureRate:  0,
	}
}

func bucketByType(t *testing.T, buckets []ToolUsageByTargetType, targetType string) ToolUsageByTargetType {
	t.Helper()
	for _, bucket := range buckets {
		if bucket.TargetType == targetType {
			return bucket
		}
	}
	require.Failf(t, "bucket missing", "no bucket for %s", targetType)
	return ToolUsageByTargetType{}
}

// TestAttributeUsageByTarget_FoldsReportedNamesOntoConfiguredServers pins the
// production shape behind "how much of our usage is shadow MCP versus hosted":
// a hosted server the pipeline attributed by toolset slug and the same server
// reported by Claude Code under a plugin prefix, which the pipeline could only
// file as shadow MCP, are one hosted entry carrying the configured id, and the
// shadow bucket holds only the names no configured server is known by.
func TestAttributeUsageByTarget_FoldsReportedNamesOntoConfiguredServers(t *testing.T) {
	t.Parallel()

	resolver := overviewResolver()
	rows := []telemetryrepo.ToolUsageTargetSummaryRow{
		usageRow(telemetryrepo.ToolUsageTargetTypeHostedMCP, "billing-tools", "billing-tools", 40, 4),
		usageRow(telemetryrepo.ToolUsageTargetTypeShadowMCP, "plugin_finance_Billing", "plugin_finance_Billing", 10, 1),
		usageRow(telemetryrepo.ToolUsageTargetTypeShadowMCP, "acme-chat", "acme-chat", 6, 0),
		usageRow(telemetryrepo.ToolUsageTargetTypeShadowMCP, "npx --yes some-package", "npx --yes some-package", 30, 3),
		usageRow(telemetryrepo.ToolUsageTargetTypeShadowMCP, "Other Shadow", "Other Shadow", 4, 0),
		usageRow(telemetryrepo.ToolUsageTargetTypeMetaMCP, "gateway-id", "Support Gateway", 5, 0),
		usageRow(telemetryrepo.ToolUsageTargetTypeLocalTool, "local", "Local Tools", 3, 0),
		usageRow(telemetryrepo.ToolUsageTargetTypeSkill, "repo-review", "repo-review", 2, 0),
	}
	serverTypes := map[string]string{overviewChatServerID.String(): telemetryrepo.ToolUsageTargetTypeTunneledMCP}

	buckets := attributeUsageByTarget(rows, resolver, serverTypes, 100, maxUsageSummaryTargets)

	types := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		types = append(types, bucket.TargetType)
	}
	require.Equal(t, usageTargetTypes, types, "every target type is reported, in a fixed order")

	hosted := bucketByType(t, buckets, telemetryrepo.ToolUsageTargetTypeHostedMCP)
	require.Equal(t, int64(50), hosted.ToolCalls)
	require.Equal(t, int64(5), hosted.FailedToolCalls)
	require.Equal(t, 1, hosted.Targets)
	require.InDelta(t, 50.0, hosted.SharePercent, 0.001)
	require.Equal(t, []ToolUsageTarget{{Name: "Billing", MCPID: overviewBillingServerID.String(), ToolCalls: 50, FailedToolCalls: 5}}, hosted.TopTargets)

	tunneled := bucketByType(t, buckets, telemetryrepo.ToolUsageTargetTypeTunneledMCP)
	require.Equal(t, int64(6), tunneled.ToolCalls)
	require.Equal(t, []ToolUsageTarget{{Name: "External Acme Chat", MCPID: overviewChatServerID.String(), ToolCalls: 6, FailedToolCalls: 0}}, tunneled.TopTargets,
		"a shadow row that resolves to a configured server lands in that server's own target type")

	shadow := bucketByType(t, buckets, telemetryrepo.ToolUsageTargetTypeShadowMCP)
	require.Equal(t, int64(34), shadow.ToolCalls)
	require.Equal(t, int64(3), shadow.FailedToolCalls)
	require.Equal(t, 2, shadow.Targets)
	require.Empty(t, shadow.TopTargets, "shadow servers are known only by the name the app used, which is not repeated")
	require.InDelta(t, 34.0, shadow.SharePercent, 0.001)

	gateway := bucketByType(t, buckets, telemetryrepo.ToolUsageTargetTypeMetaMCP)
	require.Equal(t, []ToolUsageTarget{{Name: "Support Gateway", MCPID: "", ToolCalls: 5, FailedToolCalls: 0}}, gateway.TopTargets)

	require.Equal(t, int64(3), bucketByType(t, buckets, telemetryrepo.ToolUsageTargetTypeLocalTool).ToolCalls)
	require.Equal(t, []ToolUsageTarget{{Name: "repo-review", MCPID: "", ToolCalls: 2, FailedToolCalls: 0}}, bucketByType(t, buckets, telemetryrepo.ToolUsageTargetTypeSkill).TopTargets)

	encoded, err := json.Marshal(buckets)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "npx --yes some-package")
	require.NotContains(t, string(encoded), "Other Shadow")
	require.NotContains(t, string(encoded), "plugin_finance_Billing")
}

// A toolset two configured servers wrap cannot be attributed to either, so the
// row keeps the pipeline's label and carries no id rather than guessing, and a
// shadow row under that name stays shadow rather than being moved by guess.
func TestAttributeUsageByTarget_RefusesAmbiguousNames(t *testing.T) {
	t.Parallel()

	resolver := servernames.NewResolver([]servernames.ConfiguredServer{
		{ID: overviewChatServerID.String(), Name: "Wrapper One", Slug: "wrapper-one", ToolsetSlug: "shared-tools"},
		{ID: overviewBillingServerID.String(), Name: "Wrapper Two", Slug: "wrapper-two", ToolsetSlug: "shared-tools"},
	})
	rows := []telemetryrepo.ToolUsageTargetSummaryRow{
		usageRow(telemetryrepo.ToolUsageTargetTypeHostedMCP, "shared-tools", "shared-tools", 7, 0),
		usageRow(telemetryrepo.ToolUsageTargetTypeShadowMCP, "shared-tools", "shared-tools", 2, 0),
	}

	buckets := attributeUsageByTarget(rows, resolver, nil, 9, maxUsageSummaryTargets)

	hosted := bucketByType(t, buckets, telemetryrepo.ToolUsageTargetTypeHostedMCP)
	require.Equal(t, []ToolUsageTarget{{Name: "shared-tools", MCPID: "", ToolCalls: 7, FailedToolCalls: 0}}, hosted.TopTargets)
	shadow := bucketByType(t, buckets, telemetryrepo.ToolUsageTargetTypeShadowMCP)
	require.Equal(t, int64(2), shadow.ToolCalls)
	require.Equal(t, 1, shadow.Targets)
}

func TestAttributeUsageByTarget_CapsTopTargetsAfterFolding(t *testing.T) {
	t.Parallel()

	rows := []telemetryrepo.ToolUsageTargetSummaryRow{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		rows = append(rows, usageRow(telemetryrepo.ToolUsageTargetTypeSkill, name, name, 10, 0))
	}
	rows = append(rows, usageRow(telemetryrepo.ToolUsageTargetTypeSkill, "z", "z", 90, 9))

	buckets := attributeUsageByTarget(rows, nil, nil, 160, 3)

	skills := bucketByType(t, buckets, telemetryrepo.ToolUsageTargetTypeSkill)
	require.Equal(t, 8, skills.Targets)
	require.Equal(t, int64(160), skills.ToolCalls)
	require.Len(t, skills.TopTargets, 3)
	require.Equal(t, "z", skills.TopTargets[0].Name, "ordered by calls, then name")
	require.Equal(t, "a", skills.TopTargets[1].Name)
	require.Equal(t, "b", skills.TopTargets[2].Name)
	require.InDelta(t, 100.0, skills.SharePercent, 0.001)
}

func TestAttributeUsageByTarget_WithoutRowsReportsEveryTypeAtZero(t *testing.T) {
	t.Parallel()

	buckets := attributeUsageByTarget(nil, nil, nil, 0, maxUsageSummaryTargets)

	require.Len(t, buckets, len(usageTargetTypes))
	for _, bucket := range buckets {
		require.Zero(t, bucket.ToolCalls)
		require.Zero(t, bucket.Targets)
		require.Zero(t, bucket.SharePercent)
		require.NotNil(t, bucket.TopTargets, "an empty list is emitted, not null")
	}
}

func TestConfiguredServerTargetTypes_KeysOnConfiguredIDs(t *testing.T) {
	t.Parallel()

	types := configuredServerTargetTypes(overviewResolver(), []telemetryrepo.MCPServerMatcher{
		{SourceID: "tunnel-source", TargetType: telemetryrepo.ToolUsageTargetTypeTunneledMCP, TargetID: "acme-chat", TargetLabel: "External Acme Chat"},
		{SourceID: "remote-source", TargetType: telemetryrepo.ToolUsageTargetTypeHostedMCP, TargetID: overviewBillingServerID.String(), TargetLabel: "Billing"},
		{SourceID: "unknown-source", TargetType: telemetryrepo.ToolUsageTargetTypeHostedMCP, TargetID: "not-configured", TargetLabel: "Not Configured"},
	})

	require.Equal(t, map[string]string{
		overviewChatServerID.String():    telemetryrepo.ToolUsageTargetTypeTunneledMCP,
		overviewBillingServerID.String(): telemetryrepo.ToolUsageTargetTypeHostedMCP,
	}, types)
}

func TestSharePercent_RoundsServerSide(t *testing.T) {
	t.Parallel()

	require.InDelta(t, 33.3, sharePercent(1, 3), 0.0001)
	require.InDelta(t, 66.7, sharePercent(2, 3), 0.0001)
	require.InDelta(t, 100, sharePercent(5, 5), 0.0001)
	require.Zero(t, sharePercent(0, 5))
	require.Zero(t, sharePercent(5, 0))
}

// TestUsageObserved_NeverReportsNoObservationsBesideANonzeroMetric pins the
// envelope rule: whichever number a caller reads, no_observations is false
// whenever any of them is nonzero.
func TestUsageObserved_NeverReportsNoObservationsBesideANonzeroMetric(t *testing.T) {
	t.Parallel()

	empty := attributeUsageByTarget(nil, nil, nil, 0, maxUsageSummaryTargets)
	require.False(t, usageObserved(telemetryrepo.ToolUsageTotalsRow{}, empty))

	require.True(t, usageObserved(telemetryrepo.ToolUsageTotalsRow{EventCount: 1}, empty))
	require.True(t, usageObserved(telemetryrepo.ToolUsageTotalsRow{FailureCount: 1}, empty))
	require.True(t, usageObserved(telemetryrepo.ToolUsageTotalsRow{BlockedCount: 1}, empty))
	require.True(t, usageObserved(telemetryrepo.ToolUsageTotalsRow{UniqueTools: 1}, empty))
	require.True(t, usageObserved(telemetryrepo.ToolUsageTotalsRow{UniqueTargets: 1}, empty))

	// The totals read saw nothing but the per-target read did: the buckets are
	// what the caller reads, so they count as an observation too.
	withRows := attributeUsageByTarget([]telemetryrepo.ToolUsageTargetSummaryRow{
		usageRow(telemetryrepo.ToolUsageTargetTypeLocalTool, "local", "Local Tools", 2, 0),
	}, nil, nil, 0, maxUsageSummaryTargets)
	require.True(t, usageObserved(telemetryrepo.ToolUsageTotalsRow{}, withRows))
}

// TestGetToolUsageSummaryOutput_ProjectsOnlyAllowlistedFields pins the
// serialized shape. Nothing here carries a user, a client, a session, or a raw
// reported server name.
func TestGetToolUsageSummaryOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("7d", now, usageSummaryWindowSpec)
	require.NoError(t, err)

	output := GetToolUsageSummaryOutput{
		ProjectID:        "00000000-0000-0000-0000-000000000001",
		Envelope:         newDataEnvelope(now, now.Add(-time.Minute), window, true),
		ToolCalls:        100,
		FailedToolCalls:  8,
		BlockedToolCalls: 1,
		UniqueTools:      12,
		UsageByTarget: []ToolUsageByTargetType{{
			TargetType:      telemetryrepo.ToolUsageTargetTypeHostedMCP,
			ToolCalls:       60,
			FailedToolCalls: 4,
			SharePercent:    60,
			Targets:         1,
			TopTargets:      []ToolUsageTarget{{Name: "Billing", MCPID: "00000000-0000-0000-0000-000000000002", ToolCalls: 60, FailedToolCalls: 4}},
		}},
		TargetsTruncated: false,
	}

	require.ElementsMatch(t, []string{
		"project_id",
		"data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"tool_calls", "failed_tool_calls", "blocked_tool_calls", "unique_tools",
		"usage_by_target", "target_type", "tool_calls", "failed_tool_calls", "share_percent", "targets",
		"top_targets", "name", "mcp_id", "tool_calls", "failed_tool_calls",
		"targets_truncated",
	}, decodeKeys(t, output))
}

type stubUsageSummaryTelemetry struct{}

func (stubUsageSummaryTelemetry) GetMCPOutcomeBreakdown(context.Context, telemetryrepo.GetMCPOutcomeBreakdownParams) ([]telemetryrepo.MCPOutcomeBreakdownRow, error) {
	return nil, nil
}

func (stubUsageSummaryTelemetry) GetTelemetryWatermark(context.Context, telemetryrepo.GetTelemetryWatermarkParams) (int64, error) {
	return 0, nil
}

func (stubUsageSummaryTelemetry) GetOverviewSummary(context.Context, telemetryrepo.GetOverviewSummaryParams) (*telemetryrepo.OverviewSummary, error) {
	return nil, nil
}

func (stubUsageSummaryTelemetry) GetActiveCounts(context.Context, telemetryrepo.GetActiveCountsParams) (*telemetryrepo.ActiveCounts, error) {
	return nil, nil
}

func (stubUsageSummaryTelemetry) GetTopServers(context.Context, telemetryrepo.GetTopServersParams) ([]telemetryrepo.TopServer, error) {
	return nil, nil
}

func (stubUsageSummaryTelemetry) GetSkillsSummary(context.Context, telemetryrepo.GetSkillsSummaryParams) ([]telemetryrepo.SkillSummaryRow, error) {
	return nil, nil
}

func (stubUsageSummaryTelemetry) GetSkillBreakdown(context.Context, telemetryrepo.GetSkillBreakdownParams) ([]telemetryrepo.SkillBreakdownRow, error) {
	return nil, nil
}

type stubUsageSummarySessions struct{}

func (stubUsageSummarySessions) GetActiveUserCountByMessages(context.Context, chatrepo.GetActiveUserCountByMessagesParams) (int64, error) {
	return 0, nil
}

// recordingToolUsageReader is the target-aware read with canned rows. Each
// read's parameters are recorded separately rather than into one field: the
// two reads must describe the same population of calls, which a single
// last-write-wins field cannot show. TestGetToolUsageSummaryAttributesToConfiguredServers
// asserts both, because reaching a successful read needs the project's
// matchers loaded from a real database.
type recordingToolUsageReader struct {
	totalsParams  telemetryrepo.GetToolUsageSummaryParams
	targetsParams telemetryrepo.GetToolUsageSummaryParams
	totals        telemetryrepo.ToolUsageTotalsRow
	rows          []telemetryrepo.ToolUsageTargetSummaryRow
	calls         int
}

func (r *recordingToolUsageReader) GetToolUsageTotals(_ context.Context, params telemetryrepo.GetToolUsageSummaryParams) (telemetryrepo.ToolUsageTotalsRow, error) {
	r.totalsParams = params
	r.calls++
	return r.totals, nil
}

func (r *recordingToolUsageReader) GetToolUsageTargets(_ context.Context, params telemetryrepo.GetToolUsageSummaryParams) ([]telemetryrepo.ToolUsageTargetSummaryRow, error) {
	r.targetsParams = params
	r.calls++
	return r.rows, nil
}

// unitUsageSummaryService is a service whose dependencies are all present, so
// the input checks that run before any store is touched can be exercised
// without one. Its pool is never used: every test here fails before a read.
func unitUsageSummaryService(toolUsage ToolUsageBreakdownReader) *DiagnosticsService {
	return &DiagnosticsService{
		db:             &pgxpool.Pool{},
		telemetry:      stubUsageSummaryTelemetry{},
		toolUsage:      toolUsage,
		sessions:       stubUsageSummarySessions{},
		sessionCapture: func(context.Context, string) (bool, error) { return false, nil },
		reader:         diagnosticsProjectReader{},
		budget:         OperationBudget{Connection: allowOperationLimiter{}, Organization: allowOperationLimiter{}},
		now:            func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) },
	}
}

func TestGetToolUsageSummary_RefusesWindowsOutsideTheClosedSet(t *testing.T) {
	t.Parallel()

	reader := &recordingToolUsageReader{}
	service := unitUsageSummaryService(reader)
	principal := testPrincipal()

	for _, window := range []string{"48h", "90d", "last week", "1y"} {
		_, err := service.GetToolUsageSummary(t.Context(), principal, GetToolUsageSummaryInput{ProjectID: "00000000-0000-0000-0000-000000000001", Window: window})
		require.ErrorIs(t, err, ErrDiagnosticWindowInvalid, window)
	}
	require.Zero(t, reader.calls, "a refused window never reaches the read")
}

func TestGetToolUsageSummary_RequiresAProject(t *testing.T) {
	t.Parallel()

	service := unitUsageSummaryService(&recordingToolUsageReader{})

	_, err := service.GetToolUsageSummary(t.Context(), testPrincipal(), GetToolUsageSummaryInput{})
	require.ErrorContains(t, err, "project_id is required")

	_, err = service.GetToolUsageSummary(t.Context(), testPrincipal(), GetToolUsageSummaryInput{ProjectID: "not-a-uuid"})
	require.ErrorContains(t, err, "parse project id")
}

func TestGetToolUsageSummary_IsUnavailableWithoutTheRead(t *testing.T) {
	t.Parallel()

	service := unitUsageSummaryService(nil)
	_, err := service.GetToolUsageSummary(t.Context(), testPrincipal(), GetToolUsageSummaryInput{ProjectID: "00000000-0000-0000-0000-000000000001"})
	require.ErrorIs(t, err, ErrUnavailable)

	var nilService *DiagnosticsService
	_, err = nilService.GetToolUsageSummary(t.Context(), testPrincipal(), GetToolUsageSummaryInput{ProjectID: "00000000-0000-0000-0000-000000000001"})
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestUsageSummaryWindowSpec_DefaultsToAWeekAndAllowsAMonth(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("", now, usageSummaryWindowSpec)
	require.NoError(t, err)
	require.Equal(t, DiagnosticWindowLastWeek, window.Window)

	window, err = resolveWindow("30d", now, usageSummaryWindowSpec)
	require.NoError(t, err)
	require.Equal(t, DiagnosticWindowLastMonth, window.Window)
}

// TestToolUsageSummaryStubRefuses pins the unavailable registration: the same
// name, audiences, authorization, scope, and annotations as the live tool,
// with a bounded readable refusal instead of an empty breakdown.
func TestToolUsageSummaryStubRefuses(t *testing.T) {
	t.Parallel()

	stubbed := newRegistrar(newTestMCPServer())
	registerUnavailableToolUsageSummaryTool(stubbed)
	live := newRegistrar(newTestMCPServer())
	registerToolUsageSummaryTool(live, nil)

	stub := descriptorByName(t, stubbed, toolUsageSummaryToolName)
	liveDescriptor := descriptorByName(t, live, toolUsageSummaryToolName)
	require.Equal(t, liveDescriptor.Meta, stub.Meta)
	require.Equal(t, liveDescriptor.Annotations, stub.Annotations)
	require.Equal(t, ExternalAuthorizationMember, stub.Meta.Authorization)
	require.Equal(t, bothAudiences, stub.Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, stub.Meta.ProjectScope)
	require.Equal(t, discoveryProjectRead, stub.Meta.DiscoveryScopes)
	require.True(t, stub.Annotations.ReadOnlyHint)

	_, err := stub.Invoke(ContextWithPrincipal(t.Context(), registrationServicePrincipal()), json.RawMessage(`{"project_id":"00000000-0000-0000-0000-000000000001"}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.JSONEq(t, `{"code":"feature_unavailable","feature":"tool_usage_summary","message":"This is not switched on for your organization yet."}`, refusal.Payload)

	// A deployment with no telemetry at all registers the stub, not the live
	// tool, so the catalogue never advertises a breakdown it cannot serve.
	_, deployment := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	descriptor := descriptorByName(t, deployment, toolUsageSummaryToolName)
	_, err = descriptor.Invoke(ContextWithPrincipal(t.Context(), registrationServicePrincipal()), json.RawMessage(`{"project_id":"00000000-0000-0000-0000-000000000001"}`))
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"tool_usage_summary"`)
}
