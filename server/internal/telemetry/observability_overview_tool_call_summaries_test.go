package telemetry_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// summaryParityDay keeps every parity period after the view's 2026-12-02 cutoff.
var summaryParityDay = time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)

// summaryOnlyDay is before the cutoff, so each table's rows there are seen by one path only.
var summaryOnlyDay = time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)

// latencyTolerance absorbs float summation order.
const latencyTolerance = 1e-9

type overviewLog struct {
	at         time.Time
	gramURN    string
	attributes map[string]any
}

func insertOverviewLogs(t *testing.T, ctx context.Context, conn clickhouse.Conn, projectID string, logs []overviewLog) {
	t.Helper()

	for _, l := range logs {
		id, err := uuid.NewV7()
		require.NoError(t, err)
		attrs := map[string]any{}
		maps.Copy(attrs, l.attributes)
		if l.gramURN != "" {
			attrs["gram.tool.urn"] = l.gramURN
		}
		attrsJSON, err := json.Marshal(attrs)
		require.NoError(t, err)

		err = conn.Exec(ctx, `
			INSERT INTO telemetry_logs (
				id, time_unix_nano, observed_time_unix_nano, severity_text, body,
				trace_id, span_id, attributes, resource_attributes,
				gram_project_id, gram_deployment_id, gram_urn, service_name
			) SETTINGS async_insert = 0
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, id.String(), l.at.UnixNano(), l.at.UnixNano(), "INFO", "overview log",
			nil, nil, string(attrsJSON), "{}",
			projectID, uuid.NewString(), l.gramURN, "gram-tools")
		require.NoError(t, err)
	}
}

type toolCallSummaryRow struct {
	bucket       time.Time
	gramURN      string
	mcpServerID  string
	metaServerID string
	total        uint64
	success      uint64
	failed       uint64
	latencyMsSum float64
}

func insertToolCallSummaryRows(t *testing.T, ctx context.Context, conn clickhouse.Conn, projectID string, rows []toolCallSummaryRow) {
	t.Helper()

	for _, r := range rows {
		err := conn.Exec(ctx, `
			INSERT INTO mcp_tool_call_summaries (
				gram_project_id, mcp_server_id, remote_mcp_server_id, toolset_slug,
				meta_mcp_server_id, hook_source, time_bucket, gram_urn,
				total_calls, success_calls, failed_calls, latency_ms_sum
			) SETTINGS async_insert = 0
			VALUES (?, ?, '', '', ?, '', ?, ?, ?, ?, ?, ?)
		`, projectID, r.mcpServerID, r.metaServerID, r.bucket, r.gramURN,
			r.total, r.success, r.failed, r.latencyMsSum)
		require.NoError(t, err)
	}
}

func overviewScope(projectID string, timeStart, timeEnd time.Time) repo.GetOverviewSummaryParams {
	return repo.GetOverviewSummaryParams{
		ExcludedHookSources: nil,
		GramProjectIDs:      []string{projectID},
		TimeStart:           timeStart.UnixNano(),
		TimeEnd:             timeEnd.UnixNano(),
		User:                repo.UserIdentity{UserIDs: nil, Emails: nil},
		CanonicalUser:       repo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""},
		ExternalUserID:      "",
		APIKeyID:            "",
		ToolsetSlug:         "",
		ToolsetSlugServerID: "",
		RemoteMCPServerID:   "",
		MCPServerID:         "",
		MetaMCPServerID:     "",
		EventSource:         "",
		HookSource:          "",
		AccountType:         "",
		ExternalOrgID:       "",
	}
}

func readOverview(t *testing.T, ctx context.Context, q *repo.Queries, scope repo.GetOverviewSummaryParams, toolCallsOnly, summariesEnabled bool) *repo.ObservabilityOverview {
	t.Helper()
	return readOverviewWithSeries(t, ctx, q, scope, toolCallsOnly, summariesEnabled, true)
}

func readOverviewWithSeries(t *testing.T, ctx context.Context, q *repo.Queries, scope repo.GetOverviewSummaryParams, toolCallsOnly, summariesEnabled, includeTimeSeries bool) *repo.ObservabilityOverview {
	t.Helper()

	got, err := q.GetObservabilityOverview(ctx, repo.GetObservabilityOverviewParams{
		Scope:                    scope,
		IntervalSeconds:          telemetry.CalculateInterval(scope.TimeStart, scope.TimeEnd),
		IncludeTimeSeries:        includeTimeSeries,
		ToolLimit:                10,
		ToolCallsOnly:            toolCallsOnly,
		ToolCallSummariesEnabled: summariesEnabled,
	})
	require.NoError(t, err)
	return got
}

func requireSameToolCallSummary(t *testing.T, want, got *repo.OverviewSummary) {
	t.Helper()
	require.Equal(t, want.TotalToolCalls, got.TotalToolCalls)
	require.Equal(t, want.FailedToolCalls, got.FailedToolCalls)
	require.InDelta(t, want.AvgLatencyMs, got.AvgLatencyMs, latencyTolerance)
}

func requireSameToolCallSeries(t *testing.T, want, got []repo.TimeSeriesBucket) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].BucketTimeUnixNano, got[i].BucketTimeUnixNano)
		require.Equal(t, want[i].TotalToolCalls, got[i].TotalToolCalls, "bucket %d", i)
		require.Equal(t, want[i].FailedToolCalls, got[i].FailedToolCalls, "bucket %d", i)
		require.InDelta(t, want[i].AvgToolLatencyMs, got[i].AvgToolLatencyMs, latencyTolerance, "bucket %d", i)
	}
}

// With range boundaries on 15-minute marks, both paths return the same tool figures.
func TestGetObservabilityOverview_ToolCallSummariesMatchRawLogs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()

	mcpA, mcpB := uuid.NewString(), uuid.NewString()
	remoteA, remoteB := uuid.NewString(), uuid.NewString()
	metaA, metaB := uuid.NewString(), uuid.NewString()
	tools := []string{"tools:http:a:one", "tools:http:a:two", "tools:function:b:three", "tools:http:c:four"}
	hookSources := []string{"", "cursor", "claude-code", "speakeasy-judge"}

	end := summaryParityDay.Add(18 * time.Hour)
	ranges := []struct {
		name            string
		start, end      time.Time
		endsOffTheHour  bool
		intervalSeconds int64
	}{
		{"12 hours", end.Add(-12 * time.Hour), end, false, 900},
		{"12 hours ending on a quarter hour", end.Add(-12*time.Hour - 15*time.Minute), end.Add(-15 * time.Minute), true, 900},
		{"3 days", end.Add(-72 * time.Hour), end, false, 3600},
		{"10 days", end.Add(-240 * time.Hour), end, false, 21600},
		{"40 days", end.Add(-960 * time.Hour), end, false, 86400},
	}

	// Sparse calls, dense calls past the end, and calls either side of every boundary.
	var times []time.Time
	for at := end.Add(-1920*time.Hour + 7*time.Second); at.Before(end.Add(-84 * time.Hour)); at = at.Add(7*time.Hour + 13*time.Minute) {
		times = append(times, at)
	}
	for at := end.Add(-84*time.Hour + 7*time.Second); at.Before(end.Add(30 * time.Minute)); at = at.Add(41 * time.Minute) {
		times = append(times, at)
	}
	for _, r := range ranges {
		previousStart := r.start.Add(-r.end.Sub(r.start))
		for _, boundary := range []time.Time{previousStart, r.start, r.end} {
			for _, offset := range []time.Duration{-5 * time.Minute, 3 * time.Minute, 10 * time.Minute} {
				times = append(times, boundary.Add(offset))
			}
		}
	}

	var logs []overviewLog
	for i, at := range times {
		attrs := map[string]any{
			"gram.toolset.slug": []string{"ts-a", "ts-b"}[i%2],
		}
		if s := []string{mcpA, mcpB, ""}[i%3]; s != "" {
			attrs["gram.mcp_server.id"] = s
		}
		if s := []string{remoteA, "", remoteB, ""}[(i/3)%4]; s != "" {
			attrs["gram.remote_mcp_server.id"] = s
		}
		if s := []string{metaA, "", metaB}[(i/2)%3]; s != "" {
			attrs["gram.meta_mcp_server.id"] = s
		}
		if s := hookSources[(i/2)%4]; s != "" {
			attrs["gram.hook.source"] = s
		}
		if status := []int32{200, 201, 404, 503, 0}[i%5]; status != 0 {
			attrs["http.response.status_code"] = status
		}
		if i%3 != 0 {
			attrs["http.server.request.duration"] = 0.137 * float64(i%7+1)
		}
		logs = append(logs, overviewLog{at: at, gramURN: tools[i%4], attributes: attrs})
		if i%4 == 0 {
			logs = append(logs, overviewLog{
				at:      at.Add(time.Minute),
				gramURN: "chat:resolution",
				attributes: map[string]any{
					"gram.mcp_server.id":         mcpA,
					"gen_ai.conversation.id":     uuid.NewString(),
					"gen_ai.usage.input_tokens":  100,
					"gen_ai.usage.output_tokens": 50,
					"gen_ai.usage.cost":          0.25,
				},
			})
		}
	}
	insertOverviewLogs(t, ctx, ti.chConn, projectID, logs)

	filters := []struct {
		name  string
		apply func(*repo.GetOverviewSummaryParams)
	}{
		{"project only", func(*repo.GetOverviewSummaryParams) {}},
		{"mcp server", func(s *repo.GetOverviewSummaryParams) { s.MCPServerID = mcpA }},
		{"other mcp server", func(s *repo.GetOverviewSummaryParams) { s.MCPServerID = mcpB }},
		{"remote mcp server", func(s *repo.GetOverviewSummaryParams) { s.RemoteMCPServerID = remoteA }},
		{"other remote mcp server", func(s *repo.GetOverviewSummaryParams) { s.RemoteMCPServerID = remoteB }},
		{"gateway", func(s *repo.GetOverviewSummaryParams) { s.MetaMCPServerID = metaA }},
		{"other gateway", func(s *repo.GetOverviewSummaryParams) { s.MetaMCPServerID = metaB }},
		{"toolset slug", func(s *repo.GetOverviewSummaryParams) { s.ToolsetSlug = "ts-a" }},
		{"toolset slug with canonical server", func(s *repo.GetOverviewSummaryParams) {
			s.ToolsetSlug = "ts-a"
			s.ToolsetSlugServerID = mcpA
		}},
		{"hook source", func(s *repo.GetOverviewSummaryParams) { s.HookSource = "cursor" }},
		{"excluded hook sources", func(s *repo.GetOverviewSummaryParams) {
			s.ExcludedHookSources = []string{"speakeasy-judge"}
		}},
		{"excluded hook sources with mcp server", func(s *repo.GetOverviewSummaryParams) {
			s.MCPServerID = mcpB
			s.ExcludedHookSources = []string{"speakeasy-judge"}
		}},
		{"hook source overrides exclusion", func(s *repo.GetOverviewSummaryParams) {
			s.HookSource = "speakeasy-judge"
			s.ExcludedHookSources = []string{"speakeasy-judge"}
		}},
	}

	for _, r := range ranges {
		require.Equal(t, r.intervalSeconds, telemetry.CalculateInterval(r.start.UnixNano(), r.end.UnixNano()), r.name)
		for _, f := range filters {
			// A project-only raw summary comes from metrics_summaries in whole hours.
			if r.endsOffTheHour && f.name == "project only" {
				continue
			}
			t.Run(fmt.Sprintf("%s, %s", r.name, f.name), func(t *testing.T) {
				t.Parallel()

				scope := overviewScope(projectID, r.start, r.end)
				f.apply(&scope)

				raw := readOverview(t, ctx, ti.chClient, scope, true, false)
				summary := readOverview(t, ctx, ti.chClient, scope, true, true)

				require.Equal(t, repo.OverviewSourceToolCallSummaries, summary.Source)
				require.NotEqual(t, repo.OverviewSourceToolCallSummaries, raw.Source)
				require.Positive(t, raw.Summary.TotalToolCalls, "fixture must cover this scope")
				require.Positive(t, raw.Comparison.TotalToolCalls, "fixture must cover the previous period")
				requireSameToolCallSummary(t, raw.Summary, summary.Summary)
				requireSameToolCallSummary(t, raw.Comparison, summary.Comparison)
				requireSameToolCallSeries(t, raw.TimeSeries, summary.TimeSeries)
				requireSameTools(t, raw.TopToolsByCount, summary.TopToolsByCount)
				requireSameTools(t, raw.TopToolsByFailureRate, summary.TopToolsByFailureRate)
			})
		}
		t.Run(r.name+", without time series", func(t *testing.T) {
			t.Parallel()

			scope := overviewScope(projectID, r.start, r.end)
			scope.MCPServerID = mcpA
			raw := readOverviewWithSeries(t, ctx, ti.chClient, scope, true, false, false)
			summary := readOverviewWithSeries(t, ctx, ti.chClient, scope, true, true, false)

			require.Equal(t, repo.OverviewSourceToolCallSummaries, summary.Source)
			require.Empty(t, summary.TimeSeries)
			require.Positive(t, raw.Summary.TotalToolCalls)
			requireSameToolCallSummary(t, raw.Summary, summary.Summary)
			requireSameToolCallSummary(t, raw.Comparison, summary.Comparison)
			requireSameTools(t, raw.TopToolsByCount, summary.TopToolsByCount)
			requireSameTools(t, raw.TopToolsByFailureRate, summary.TopToolsByFailureRate)
		})
	}
}

// An unaligned range snaps to whole buckets, with equal-length periods.
func TestGetObservabilityOverview_ToolCallSummariesSnapUnalignedRange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()

	at := func(h, m int) time.Time {
		return summaryParityDay.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	}
	// One call per bucket from 03:00 to 17:00.
	var logs []overviewLog
	for ts := at(3, 1); ts.Before(at(17, 0)); ts = ts.Add(15 * time.Minute) {
		logs = append(logs, overviewLog{at: ts, gramURN: "tools:http:a:one", attributes: map[string]any{"http.response.status_code": 200}})
	}
	insertOverviewLogs(t, ctx, ti.chConn, projectID, logs)

	// Current [10:00, 16:15) and previous [03:45, 10:00): 25 buckets each.
	got := readOverview(t, ctx, ti.chClient, overviewScope(projectID, at(10, 7), at(16, 7)), true, true)
	require.Equal(t, repo.OverviewSourceToolCallSummaries, got.Source)
	require.Equal(t, uint64(25), got.Summary.TotalToolCalls)
	require.Equal(t, got.Summary.TotalToolCalls, got.Comparison.TotalToolCalls)

	var seriesCalls uint64
	for _, b := range got.TimeSeries {
		seriesCalls += b.TotalToolCalls
	}
	require.Equal(t, uint64(25), seriesCalls)
	require.Equal(t, at(10, 0).UnixNano(), got.TimeSeries[0].BucketTimeUnixNano)
	require.Equal(t, at(16, 0).UnixNano(), got.TimeSeries[len(got.TimeSeries)-1].BucketTimeUnixNano)
}

// Organization gateway rows share the nil project, so only the requested gateway's rows count.
func TestGetObservabilityOverview_ToolCallSummariesNilProjectGateway(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	gateway, otherGateway := uuid.NewString(), uuid.NewString()

	calls := func(n int, gatewayID string) []overviewLog {
		var logs []overviewLog
		for i := range n {
			logs = append(logs, overviewLog{
				at:         summaryParityDay.Add(time.Duration(i+1) * time.Hour),
				gramURN:    "tools:http:a:one",
				attributes: map[string]any{"gram.meta_mcp_server.id": gatewayID, "http.response.status_code": 200},
			})
		}
		return logs
	}
	insertOverviewLogs(t, ctx, ti.chConn, projectID, calls(3, gateway))
	insertOverviewLogs(t, ctx, ti.chConn, uuid.Nil.String(), calls(2, gateway))
	insertOverviewLogs(t, ctx, ti.chConn, uuid.Nil.String(), calls(7, otherGateway))

	scope := overviewScope(projectID, summaryParityDay, summaryParityDay.Add(12*time.Hour))
	scope.GramProjectIDs = []string{projectID, uuid.Nil.String()}
	scope.MetaMCPServerID = gateway

	raw := readOverview(t, ctx, ti.chClient, scope, true, false)
	summary := readOverview(t, ctx, ti.chClient, scope, true, true)
	require.Equal(t, repo.OverviewSourceToolCallSummaries, summary.Source)
	require.Equal(t, uint64(5), summary.Summary.TotalToolCalls)
	require.Equal(t, raw.Summary.TotalToolCalls, summary.Summary.TotalToolCalls)
}

// Each table holds different calls (raw 2, summary 5), so the total shows which path ran.
func TestGetObservabilityOverview_ToolCallSummariesPathSelection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	mcpServerID, metaServerID := uuid.NewString(), uuid.NewString()

	const rawCalls, summaryCalls = 2, 5
	rawAttrs := map[string]any{
		"gram.mcp_server.id":           mcpServerID,
		"gram.meta_mcp_server.id":      metaServerID,
		"http.response.status_code":    200,
		"user.id":                      "user-1",
		"gram.external_user.id":        "external-1",
		"gram.api_key.id":              "key-1",
		"gram.event.source":            "hook",
		"gram.account_type":            "team",
		"gram.external_org_id":         "external-org-1",
		"http.server.request.duration": 0.5,
	}
	insertOverviewLogs(t, ctx, ti.chConn, projectID, []overviewLog{
		{at: summaryOnlyDay.Add(time.Hour), gramURN: "tools:http:a:one", attributes: rawAttrs},
		{at: summaryOnlyDay.Add(2 * time.Hour), gramURN: "tools:http:a:one", attributes: rawAttrs},
	})
	insertToolCallSummaryRows(t, ctx, ti.chConn, projectID, []toolCallSummaryRow{
		{bucket: summaryOnlyDay.Add(time.Hour), gramURN: "tools:http:a:one", mcpServerID: mcpServerID, metaServerID: metaServerID, total: summaryCalls, success: summaryCalls, failed: 0, latencyMsSum: 2500},
	})

	timeStart, timeEnd := summaryOnlyDay, summaryOnlyDay.Add(12*time.Hour)
	base := func() repo.GetOverviewSummaryParams {
		s := overviewScope(projectID, timeStart, timeEnd)
		s.MCPServerID = mcpServerID
		return s
	}

	cases := []struct {
		name             string
		scope            func() repo.GetOverviewSummaryParams
		toolCallsOnly    bool
		summariesEnabled bool
		want             uint64
	}{
		{"all conditions hold", base, true, true, summaryCalls},
		{"flag off", base, true, false, rawCalls},
		{"tool_calls_only false", base, false, true, rawCalls},
		{"range of one hour", func() repo.GetOverviewSummaryParams {
			s := base()
			s.TimeStart = summaryOnlyDay.Add(30 * time.Minute).UnixNano()
			s.TimeEnd = summaryOnlyDay.Add(90 * time.Minute).UnixNano()
			return s
		}, true, true, 1},
		{"missing from", func() repo.GetOverviewSummaryParams {
			s := base()
			s.TimeStart = 0
			return s
		}, true, true, rawCalls},
		{"previous period before the epoch", func() repo.GetOverviewSummaryParams {
			s := base()
			s.TimeStart = time.Date(1970, 1, 1, 2, 0, 0, 0, time.UTC).UnixNano()
			s.TimeEnd = time.Date(1970, 1, 1, 4, 1, 0, 0, time.UTC).UnixNano()
			return s
		}, true, true, 0},
		{"user", func() repo.GetOverviewSummaryParams {
			s := base()
			s.User = repo.UserIdentity{UserIDs: []string{"user-1"}, Emails: nil}
			return s
		}, true, true, rawCalls},
		{"canonical user", func() repo.GetOverviewSummaryParams {
			s := base()
			s.CanonicalUser = repo.CanonicalUserIdentity{OrgID: authCtx.ActiveOrganizationID, UserID: "user-1", EmailLower: ""}
			return s
		}, true, true, rawCalls},
		{"external user", func() repo.GetOverviewSummaryParams {
			s := base()
			s.ExternalUserID = "external-1"
			return s
		}, true, true, rawCalls},
		{"api key", func() repo.GetOverviewSummaryParams {
			s := base()
			s.APIKeyID = "key-1"
			return s
		}, true, true, rawCalls},
		{"event source", func() repo.GetOverviewSummaryParams {
			s := base()
			s.EventSource = "hook"
			return s
		}, true, true, rawCalls},
		{"account type", func() repo.GetOverviewSummaryParams {
			s := base()
			s.AccountType = "team"
			return s
		}, true, true, rawCalls},
		{"external org", func() repo.GetOverviewSummaryParams {
			s := base()
			s.ExternalOrgID = "external-org-1"
			return s
		}, true, true, rawCalls},
		{"nil project without gateway", func() repo.GetOverviewSummaryParams {
			s := base()
			s.GramProjectIDs = []string{projectID, uuid.Nil.String()}
			return s
		}, true, true, rawCalls},
		{"nil project with gateway", func() repo.GetOverviewSummaryParams {
			s := overviewScope(projectID, timeStart, timeEnd)
			s.GramProjectIDs = []string{projectID, uuid.Nil.String()}
			s.MetaMCPServerID = metaServerID
			return s
		}, true, true, summaryCalls},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := readOverview(t, ctx, ti.chClient, tc.scope(), tc.toolCallsOnly, tc.summariesEnabled)
			require.Equal(t, tc.want, got.Summary.TotalToolCalls)
			require.Equal(t, tc.want == summaryCalls, got.Source == repo.OverviewSourceToolCallSummaries)
		})
	}
}

// tool_calls_only zeroes every non-tool figure on both paths.
func TestGetObservabilityOverview_ToolCallsOnlyZeroesOtherFigures(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	mcpServerID := uuid.NewString()

	chat := func(at time.Time) overviewLog {
		return overviewLog{at: at, gramURN: "chat:resolution", attributes: map[string]any{
			"gram.mcp_server.id":            mcpServerID,
			"gen_ai.conversation.id":        uuid.NewString(),
			"gen_ai.conversation.duration":  30.0,
			"gen_ai.evaluation.score.label": "success",
			"gen_ai.usage.input_tokens":     100,
			"gen_ai.usage.output_tokens":    50,
			"gen_ai.usage.total_tokens":     150,
			"gen_ai.usage.cost":             0.25,
		}}
	}
	call := func(at time.Time) overviewLog {
		return overviewLog{at: at, gramURN: "tools:http:a:one", attributes: map[string]any{
			"gram.mcp_server.id":        mcpServerID,
			"http.response.status_code": 500,
		}}
	}
	insertOverviewLogs(t, ctx, ti.chConn, projectID, []overviewLog{
		chat(summaryParityDay.Add(time.Hour)), call(summaryParityDay.Add(time.Hour)),
		chat(summaryParityDay.Add(-5 * time.Hour)), call(summaryParityDay.Add(-5 * time.Hour)),
	})

	scope := overviewScope(projectID, summaryParityDay, summaryParityDay.Add(6*time.Hour))
	scope.MCPServerID = mcpServerID

	full := readOverview(t, ctx, ti.chClient, scope, false, true)
	require.Equal(t, uint64(1), full.Summary.TotalChats)
	require.Positive(t, full.Summary.TotalTokens)
	require.Positive(t, full.Comparison.TotalCost)

	for name, summariesEnabled := range map[string]bool{"raw path": false, "summary path": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := readOverview(t, ctx, ti.chClient, scope, true, summariesEnabled)
			for _, s := range []*repo.OverviewSummary{got.Summary, got.Comparison} {
				require.Equal(t, repo.OverviewSummary{
					TotalChats:               0,
					ResolvedChats:            0,
					FailedChats:              0,
					AvgSessionDurationMs:     0,
					AvgResolutionTimeMs:      0,
					TotalInputTokens:         0,
					TotalOutputTokens:        0,
					TotalTokens:              0,
					CacheReadInputTokens:     0,
					CacheCreationInputTokens: 0,
					TotalCost:                0,
					TotalToolCalls:           1,
					FailedToolCalls:          1,
					AvgLatencyMs:             0,
				}, *s)
			}
			var seriesCalls uint64
			for _, b := range got.TimeSeries {
				require.Zero(t, b.TotalChats)
				require.Zero(t, b.ResolvedChats)
				require.Zero(t, b.TotalInputTokens)
				require.Zero(t, b.TotalOutputTokens)
				require.Zero(t, b.TotalTokens)
				require.Zero(t, b.TotalCost)
				require.Zero(t, b.AvgSessionDurationMs)
				seriesCalls += b.TotalToolCalls
			}
			require.Equal(t, uint64(1), seriesCalls)
		})
	}
}

// The service reads the flag for the caller's organization.
func TestGetObservabilityOverview_ToolCallSummariesFlag(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	mcpServerID := uuid.NewString()

	insertToolCallSummaryRows(t, ctx, ti.chConn, projectID, []toolCallSummaryRow{
		{bucket: summaryOnlyDay.Add(time.Hour), gramURN: "tools:http:a:one", mcpServerID: mcpServerID, metaServerID: "", total: 5, success: 4, failed: 1, latencyMsSum: 1000},
	})

	read := func(toolCallsOnly bool) *gen.GetObservabilityOverviewResult {
		res, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
			From:              summaryOnlyDay.Format(time.RFC3339),
			To:                summaryOnlyDay.Add(12 * time.Hour).Format(time.RFC3339),
			McpServerID:       &mcpServerID,
			IncludeTimeSeries: true,
			ToolCallsOnly:     toolCallsOnly,
		})
		require.NoError(t, err)
		return res
	}

	require.Equal(t, int64(0), read(true).Summary.TotalToolCalls, "flag missing reads the raw path")

	ti.features.SetFlag(feature.FlagMCPOverviewSummaryTable, authCtx.ActiveOrganizationID, false)
	require.Equal(t, int64(0), read(true).Summary.TotalToolCalls, "flag off reads the raw path")

	ti.features.SetFlag(feature.FlagMCPOverviewSummaryTable, authCtx.ActiveOrganizationID, true)
	res := read(true)
	require.Equal(t, int64(5), res.Summary.TotalToolCalls)
	require.Equal(t, int64(1), res.Summary.FailedToolCalls)
	require.InDelta(t, 200.0, res.Summary.AvgLatencyMs, latencyTolerance)
	require.Equal(t, int64(0), read(false).Summary.TotalToolCalls, "tool_calls_only false reads the raw path")
}
