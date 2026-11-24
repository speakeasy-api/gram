package telemetry_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// summaryParityDay is after the mcp_tool_call_summaries view's cutoff
// (2026-12-02 00:00 UTC), so telemetry_logs rows reach the table.
var summaryParityDay = time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC)

// summaryOnlyDay is before that cutoff: telemetry_logs rows there never reach
// the table, so rows inserted into each table directly are seen by one path only.
var summaryOnlyDay = time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)

// latencyTolerance absorbs float summation order: the table sums latency per
// bucket before the read sums the buckets.
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
		for k, v := range l.attributes {
			attrs[k] = v
		}
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

// overviewIntervalSeconds mirrors calculateInterval for the ranges used here.
func overviewIntervalSeconds(timeStart, timeEnd time.Time) int64 {
	hours := int64(timeEnd.Sub(timeStart) / time.Hour)
	switch {
	case hours <= 1:
		return 60
	case hours <= 24:
		return 900
	case hours <= 168:
		return 3600
	case hours <= 720:
		return 21600
	default:
		return 86400
	}
}

func readOverview(t *testing.T, ctx context.Context, q *repo.Queries, scope repo.GetOverviewSummaryParams, toolCallsOnly, summariesEnabled bool) *repo.ObservabilityOverview {
	t.Helper()

	got, err := q.GetObservabilityOverview(ctx, repo.GetObservabilityOverviewParams{
		Scope:                    scope,
		IntervalSeconds:          overviewIntervalSeconds(time.Unix(0, scope.TimeStart), time.Unix(0, scope.TimeEnd)),
		IncludeTimeSeries:        true,
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

// The summary path must return the raw path's tool figures for every filter
// the table supports, given range boundaries on 15-minute marks.
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

	var logs []overviewLog
	// Rows every 41 minutes from 2.5 days before summaryParityDay up to 18:00
	// on it, so they land in both periods of every range below and none falls
	// after a range's end.
	first := summaryParityDay.Add(-60*time.Hour + 7*time.Second)
	for i := 0; first.Add(time.Duration(i) * 41 * time.Minute).Before(summaryParityDay.Add(18 * time.Hour)); i++ {
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
		logs = append(logs, overviewLog{at: first.Add(time.Duration(i) * 41 * time.Minute), gramURN: tools[i%4], attributes: attrs})
		if i%4 == 0 {
			logs = append(logs, overviewLog{
				at:      first.Add(time.Duration(i)*41*time.Minute + time.Minute),
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

	ranges := map[string][2]time.Time{
		"12 hours": {summaryParityDay.Add(6 * time.Hour), summaryParityDay.Add(18 * time.Hour)},
		"3 days":   {summaryParityDay.Add(-54 * time.Hour), summaryParityDay.Add(18 * time.Hour)},
	}
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

	for rangeName, r := range ranges {
		for _, f := range filters {
			t.Run(fmt.Sprintf("%s, %s", rangeName, f.name), func(t *testing.T) {
				t.Parallel()

				scope := overviewScope(projectID, r[0], r[1])
				f.apply(&scope)

				raw := readOverview(t, ctx, ti.chClient, scope, true, false)
				summary := readOverview(t, ctx, ti.chClient, scope, true, true)

				require.Positive(t, raw.Summary.TotalToolCalls, "fixture must cover this scope")
				require.Positive(t, raw.Comparison.TotalToolCalls, "fixture must cover the previous period")
				requireSameToolCallSummary(t, raw.Summary, summary.Summary)
				requireSameToolCallSummary(t, raw.Comparison, summary.Comparison)
				requireSameToolCallSeries(t, raw.TimeSeries, summary.TimeSeries)
				requireSameTools(t, raw.TopToolsByCount, summary.TopToolsByCount)
				requireSameTools(t, raw.TopToolsByFailureRate, summary.TopToolsByFailureRate)
			})
		}
	}
}

// An unaligned range snaps to the table's 15-minute buckets.
func TestGetObservabilityOverview_ToolCallSummariesSnapUnalignedRange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()

	timeStart := summaryParityDay.Add(10*time.Hour + 7*time.Minute)
	timeEnd := summaryParityDay.Add(16*time.Hour + 7*time.Minute)
	at := func(h, m int) time.Time {
		return summaryParityDay.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
	}
	call := func(ts time.Time) overviewLog {
		return overviewLog{at: ts, gramURN: "tools:http:a:one", attributes: map[string]any{"http.response.status_code": 200}}
	}
	insertOverviewLogs(t, ctx, ti.chConn, projectID, []overviewLog{
		// Current period: the bucket at 10:00 is in, though 10:02 is before
		// TimeStart, and so is the bucket at 16:00, though 16:10 is after TimeEnd.
		call(at(10, 2)),
		call(at(12, 0)),
		call(at(16, 10)),
		// After the bucket holding TimeEnd: out.
		call(at(16, 15)),
		// Previous period: [04:00, 10:00), from floor15(10:00 - 6h).
		call(at(4, 0)),
		call(at(9, 59)),
		// Before the previous period: out.
		call(at(3, 58)),
	})

	got := readOverview(t, ctx, ti.chClient, overviewScope(projectID, timeStart, timeEnd), true, true)
	require.Equal(t, uint64(3), got.Summary.TotalToolCalls)
	require.Equal(t, uint64(2), got.Comparison.TotalToolCalls)

	var seriesCalls uint64
	for _, b := range got.TimeSeries {
		seriesCalls += b.TotalToolCalls
	}
	require.Equal(t, uint64(3), seriesCalls)
	require.Equal(t, at(10, 0).UnixNano(), got.TimeSeries[0].BucketTimeUnixNano)
	require.Equal(t, at(16, 0).UnixNano(), got.TimeSeries[len(got.TimeSeries)-1].BucketTimeUnixNano)
}

// Rows here are in only one table each: two calls in telemetry_logs before the
// view's cutoff, five calls inserted into mcp_tool_call_summaries. The total
// tells which path ran.
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
		})
	}
}

// tool_calls_only zeroes every chat, token, cost and session figure on both
// paths, and leaves them alone when false.
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
