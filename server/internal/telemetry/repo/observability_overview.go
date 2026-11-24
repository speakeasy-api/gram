package repo

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
)

// GetObservabilityOverviewParams scopes one observability overview read.
type GetObservabilityOverviewParams struct {
	// Scope is the filter set and the current period [TimeStart, TimeEnd].
	Scope GetOverviewSummaryParams
	// IntervalSeconds is the time series bucket width.
	IntervalSeconds int64
	// IncludeTimeSeries adds the bucketed series to the result.
	IncludeTimeSeries bool
	// ToolLimit caps each of the two top-tools lists.
	ToolLimit int

	// ToolCallsOnly zeroes every figure that is not about tool calls.
	ToolCallsOnly bool

	// ToolCallSummariesEnabled lets an eligible read use mcp_tool_call_summaries.
	ToolCallSummariesEnabled bool
}

// toolCallSummaryBucketNanos is the mcp_tool_call_summaries bucket width.
const toolCallSummaryBucketNanos = int64(15 * time.Minute)

// ToolCallSummariesEligible reports whether mcp_tool_call_summaries can serve the read.
func (arg GetObservabilityOverviewParams) ToolCallSummariesEligible() bool {
	return arg.ToolCallsOnly &&
		arg.IntervalSeconds*int64(time.Second) >= toolCallSummaryBucketNanos &&
		toolCallSummariesPreviousStart(arg.Scope.TimeStart, arg.Scope.TimeEnd) >= 0 &&
		arg.Scope.toolCallSummariesCanServe()
}

// toolCallSummariesPreviousStart is negative when a range starts near the epoch.
func toolCallSummariesPreviousStart(timeStart, timeEnd int64) int64 {
	return floorToolCallSummaryBucket(floorToolCallSummaryBucket(timeStart) - (timeEnd - timeStart))
}

func floorToolCallSummaryBucket(unixNano int64) int64 {
	return (unixNano / toolCallSummaryBucketNanos) * toolCallSummaryBucketNanos
}

func ceilToolCallSummaryBucket(unixNano int64) int64 {
	return floorToolCallSummaryBucket(unixNano + toolCallSummaryBucketNanos - 1)
}

// OverviewSource names the table that served an overview.
type OverviewSource string

const (
	OverviewSourceToolCallSummaries OverviewSource = "tool_call_summaries"
	OverviewSourceTelemetryLogs     OverviewSource = "telemetry_logs"
	// OverviewSourceMetricsSummaries serves the summaries; the series and tools still scan telemetry_logs.
	OverviewSourceMetricsSummaries OverviewSource = "metrics_summaries"
)

// ObservabilityOverview is everything the overview shows for one scope.
type ObservabilityOverview struct {
	Summary *OverviewSummary
	// Comparison is the same summary for the period of equal length that ends
	// where the current one starts.
	Comparison            *OverviewSummary
	TimeSeries            []TimeSeriesBucket
	TopToolsByCount       []ToolMetric
	TopToolsByFailureRate []ToolMetric
	Source                OverviewSource
}

// overviewRow is one row of the grouped overview query. Which grouping set it
// came from is told by its keys: a bucket row has a bucket, a tool row has a
// tool, and a summary row has neither.
type overviewRow struct {
	Period    uint8  `ch:"period"`
	BucketKey int64  `ch:"bucket_key"`
	ToolKey   string `ch:"tool_key"`

	TotalChats           uint64  `ch:"total_chats"`
	ResolvedChats        uint64  `ch:"resolved_chats"`
	FailedChats          uint64  `ch:"failed_chats"`
	PartialChats         uint64  `ch:"partial_chats"`
	AbandonedChats       uint64  `ch:"abandoned_chats"`
	AvgSessionDurationMs float64 `ch:"avg_session_duration_ms"`
	AvgResolutionTimeMs  float64 `ch:"avg_resolution_time_ms"`

	TotalInputTokens         int64   `ch:"total_input_tokens"`
	TotalOutputTokens        int64   `ch:"total_output_tokens"`
	TotalTokens              int64   `ch:"total_tokens"`
	CacheReadInputTokens     int64   `ch:"cache_read_input_tokens"`
	CacheCreationInputTokens int64   `ch:"cache_creation_input_tokens"`
	TotalCost                float64 `ch:"total_cost"`

	TotalToolCalls   uint64  `ch:"total_tool_calls"`
	SuccessToolCalls uint64  `ch:"success_tool_calls"`
	FailedToolCalls  uint64  `ch:"failed_tool_calls"`
	AvgToolLatencyMs float64 `ch:"avg_tool_latency_ms"`
}

const (
	overviewPeriodPrevious uint8 = 0
	overviewPeriodCurrent  uint8 = 1

	// overviewOtherTool keys the rows that are not tool calls in the per-tool
	// grouping set, so they cannot be mistaken for a summary row (empty key).
	overviewOtherTool = "~"
)

// GetObservabilityOverview reads the summary, the previous period's summary,
// the time series, and the top tools for one scope in a single pass over
// telemetry_logs, using GROUPING SETS. Before, these were five separate scans.
//
// An unfiltered scope still takes both summaries from the metrics_summaries
// view, which is cheaper than any raw scan; the single pass then covers only
// the current period for the series and the tools.
//
// An eligible read with ToolCallSummariesEnabled reads mcp_tool_call_summaries instead.
func (q *Queries) GetObservabilityOverview(ctx context.Context, arg GetObservabilityOverviewParams) (*ObservabilityOverview, error) {
	if arg.ToolCallSummariesEnabled && arg.ToolCallSummariesEligible() {
		return q.getObservabilityOverviewFromToolCallSummaries(ctx, arg)
	}

	result, err := q.getObservabilityOverviewFromLogs(ctx, arg)
	if err != nil {
		return nil, err
	}
	if arg.ToolCallsOnly {
		result.keepToolCallFiguresOnly()
	}
	return result, nil
}

// getObservabilityOverviewFromLogs reads the overview from telemetry_logs.
func (q *Queries) getObservabilityOverviewFromLogs(ctx context.Context, arg GetObservabilityOverviewParams) (*ObservabilityOverview, error) {
	scope := arg.Scope
	timeStart, timeEnd := scope.TimeStart, scope.TimeEnd
	previousStart := timeStart - (timeEnd - timeStart)

	summariesFromView := !scope.hasFilters()
	result := &ObservabilityOverview{
		Summary:               new(OverviewSummary),
		Comparison:            new(OverviewSummary),
		TimeSeries:            nil,
		TopToolsByCount:       nil,
		TopToolsByFailureRate: nil,
		Source:                OverviewSourceTelemetryLogs,
	}
	if summariesFromView {
		result.Source = OverviewSourceMetricsSummaries
		summary, err := q.GetOverviewSummary(ctx, scope)
		if err != nil {
			return nil, err
		}
		comparisonScope := scope
		comparisonScope.TimeStart = previousStart
		comparisonScope.TimeEnd = timeStart
		comparison, err := q.GetOverviewSummary(ctx, comparisonScope)
		if err != nil {
			return nil, err
		}
		result.Summary, result.Comparison = summary, comparison
	}

	scanStart := previousStart
	if summariesFromView {
		scanStart = timeStart
	}

	// Every selected key must be in some grouping set, so without a series
	// the bucket key is a constant.
	bucketKey := squirrel.Expr("toInt64(0) AS bucket_key")
	if arg.IncludeTimeSeries {
		bucketKey = squirrel.Expr(
			"if(time_unix_nano >= ?, toInt64(toStartOfInterval(fromUnixTimestamp64Nano(time_unix_nano), toIntervalSecond(?))) * 1000000000, -1) AS bucket_key",
			timeStart, arg.IntervalSeconds,
		)
	}

	// The previous period is [previousStart, timeStart) and the current one
	// [timeStart, timeEnd], so no row is counted in both.
	sb := sq.Select().
		Column(squirrel.Expr("if(time_unix_nano >= ?, 1, 0) AS period", timeStart)).
		// Previous-period rows only feed the summaries, so they collapse to one
		// throwaway bucket and tool key instead of building per-bucket and
		// per-tool aggregates nobody reads.
		Column(bucketKey).
		Column(squirrel.Expr(
			"if(time_unix_nano >= ? AND startsWith(gram_urn, 'tools:'), gram_urn, '"+overviewOtherTool+"') AS tool_key",
			timeStart,
		)).
		Columns(
			"uniqExactIf(chat_id, chat_id != '') AS total_chats",
			"uniqExactIf(chat_id, chat_id != '' AND evaluation_score_label = 'success') AS resolved_chats",
			"uniqExactIf(chat_id, chat_id != '' AND evaluation_score_label = 'failure') AS failed_chats",
			"uniqExactIf(chat_id, chat_id != '' AND evaluation_score_label = 'partial') AS partial_chats",
			"uniqExactIf(chat_id, chat_id != '' AND evaluation_score_label = 'abandoned') AS abandoned_chats",
			"if(isNaN(avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gen_ai.conversation.duration) != '')), 0, avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gen_ai.conversation.duration) != '')) AS avg_session_duration_ms",
			"if(isNaN(avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, evaluation_score_label = 'success')), 0, avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, evaluation_score_label = 'success')) AS avg_resolution_time_ms",
			"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') AS total_input_tokens",
			"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') AS total_output_tokens",
			"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens",
			"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '') AS cache_read_input_tokens",
			"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '') AS cache_creation_input_tokens",
			"sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') AS total_cost",
			"countIf(startsWith(gram_urn, 'tools:')) AS total_tool_calls",
			"countIf(startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS success_tool_calls",
			"countIf(startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS failed_tool_calls",
			"if(isNaN(avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(gram_urn, 'tools:'))), 0, avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(gram_urn, 'tools:'))) AS avg_tool_latency_ms",
		).
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": scope.GramProjectIDs}).
		Where("time_unix_nano >= ?", scanStart).
		Where("time_unix_nano <= ?", timeEnd)
	sb = withOverviewScopeFilters(sb, scope)

	sets := []string{"(period, tool_key)"}
	if !summariesFromView {
		sets = append(sets, "(period)")
	}
	if arg.IncludeTimeSeries {
		sets = append(sets, "(period, bucket_key)")
	}
	sb = sb.GroupBy("GROUPING SETS (" + strings.Join(sets, ", ") + ")")
	sb = withCanonicalFoldSettings(sb, scope.CanonicalUser.orgLit())

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building observability overview query: %w", err)
	}

	if err := q.readOverviewRows(ctx, query, args, arg, result); err != nil {
		return nil, err
	}
	return result, nil
}

// Periods snap to whole buckets: [floor15(start), ceil15(end)) and the equal span before it.
func (q *Queries) getObservabilityOverviewFromToolCallSummaries(ctx context.Context, arg GetObservabilityOverviewParams) (*ObservabilityOverview, error) {
	scope := arg.Scope
	currentStartSeconds := floorToolCallSummaryBucket(scope.TimeStart) / int64(time.Second)
	previousStart := toolCallSummariesPreviousStart(scope.TimeStart, scope.TimeEnd)

	bucketKey := squirrel.Expr("toInt64(0) AS bucket_key")
	if arg.IncludeTimeSeries {
		bucketKey = squirrel.Expr(
			"if(time_bucket >= toDateTime(?, 'UTC'), toInt64(toStartOfInterval(time_bucket, toIntervalSecond(?))) * 1000000000, -1) AS bucket_key",
			currentStartSeconds, arg.IntervalSeconds,
		)
	}

	sb := sq.Select().
		Column(squirrel.Expr("if(time_bucket >= toDateTime(?, 'UTC'), 1, 0) AS period", currentStartSeconds)).
		Column(bucketKey).
		Column(squirrel.Expr(
			"if(time_bucket >= toDateTime(?, 'UTC'), gram_urn, '"+overviewOtherTool+"') AS tool_key",
			currentStartSeconds,
		)).
		Columns(
			"sum(total_calls) AS total_tool_calls",
			"sum(success_calls) AS success_tool_calls",
			"sum(failed_calls) AS failed_tool_calls",
			"if(isNaN(sum(latency_ms_sum) / sum(total_calls)), 0, sum(latency_ms_sum) / sum(total_calls)) AS avg_tool_latency_ms",
		).
		From("mcp_tool_call_summaries").
		Where(squirrel.Eq{"gram_project_id": scope.GramProjectIDs}).
		Where("time_bucket >= toDateTime(?, 'UTC')", previousStart/int64(time.Second)).
		Where("time_bucket < toDateTime(?, 'UTC')", ceilToolCallSummaryBucket(scope.TimeEnd)/int64(time.Second))
	sb = withOverviewScopeFilters(sb, scope)

	sets := []string{"(period, tool_key)", "(period)"}
	if arg.IncludeTimeSeries {
		sets = append(sets, "(period, bucket_key)")
	}
	sb = sb.GroupBy("GROUPING SETS (" + strings.Join(sets, ", ") + ")")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool call summaries overview query: %w", err)
	}

	result := &ObservabilityOverview{
		Summary:               new(OverviewSummary),
		Comparison:            new(OverviewSummary),
		TimeSeries:            nil,
		TopToolsByCount:       nil,
		TopToolsByFailureRate: nil,
		Source:                OverviewSourceToolCallSummaries,
	}
	if err := q.readOverviewRows(ctx, query, args, arg, result); err != nil {
		return nil, err
	}
	return result, nil
}

// readOverviewRows sorts a grouped overview query's rows into the result.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) readOverviewRows(ctx context.Context, query string, args []any, arg GetObservabilityOverviewParams, result *ObservabilityOverview) error {
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	buckets := map[int64]TimeSeriesBucket{}
	var tools []ToolMetric
	for rows.Next() {
		var row overviewRow
		if err = rows.ScanStruct(&row); err != nil {
			return fmt.Errorf("scanning observability overview row: %w", err)
		}
		switch {
		case row.BucketKey != 0:
			if row.BucketKey > 0 {
				buckets[row.BucketKey] = row.bucket()
			}
		case row.ToolKey != "":
			if row.ToolKey != overviewOtherTool {
				tools = append(tools, row.toolMetric())
			}
		case row.Period == overviewPeriodCurrent:
			result.Summary = row.summary()
		case row.Period == overviewPeriodPrevious:
			result.Comparison = row.summary()
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}

	if arg.IncludeTimeSeries {
		result.TimeSeries = fillTimeSeries(buckets, arg.Scope.TimeStart, arg.Scope.TimeEnd, arg.IntervalSeconds)
	}
	result.TopToolsByCount, result.TopToolsByFailureRate = topTools(tools, arg.ToolLimit)
	return nil
}

// keepToolCallFiguresOnly zeroes every figure that is not about tool calls.
func (o *ObservabilityOverview) keepToolCallFiguresOnly() {
	for _, summary := range []*OverviewSummary{o.Summary, o.Comparison} {
		*summary = OverviewSummary{
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
			TotalToolCalls:           summary.TotalToolCalls,
			FailedToolCalls:          summary.FailedToolCalls,
			AvgLatencyMs:             summary.AvgLatencyMs,
		}
	}
	for i, bucket := range o.TimeSeries {
		o.TimeSeries[i] = TimeSeriesBucket{
			BucketTimeUnixNano:       bucket.BucketTimeUnixNano,
			TotalChats:               0,
			ResolvedChats:            0,
			FailedChats:              0,
			PartialChats:             0,
			AbandonedChats:           0,
			TotalInputTokens:         0,
			TotalOutputTokens:        0,
			TotalTokens:              0,
			CacheReadInputTokens:     0,
			CacheCreationInputTokens: 0,
			TotalCost:                0,
			TotalToolCalls:           bucket.TotalToolCalls,
			FailedToolCalls:          bucket.FailedToolCalls,
			AvgToolLatencyMs:         bucket.AvgToolLatencyMs,
			AvgSessionDurationMs:     0,
		}
	}
}

// withOverviewScopeFilters applies the overview's optional filters. It
// matches the filters GetOverviewSummary, GetTimeSeriesMetrics and
// GetToolMetricsBreakdown apply.
func withOverviewScopeFilters(sb squirrel.SelectBuilder, arg GetOverviewSummaryParams) squirrel.SelectBuilder {
	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{userIdentifierExpr("external_user_id"): arg.ExternalUserID})
	}
	sb = withUserIdentityFilter(sb, arg.User, arg.CanonicalUser)
	if arg.APIKeyID != "" {
		sb = sb.Where(squirrel.Eq{"api_key_id": arg.APIKeyID})
	}
	if arg.ToolsetSlug != "" {
		sb = sb.Where(squirrel.Eq{"toolset_slug": arg.ToolsetSlug})
		if arg.ToolsetSlugServerID != "" {
			sb = sb.Where(squirrel.Eq{"mcp_server_id": []string{"", arg.ToolsetSlugServerID}})
		}
	}
	if arg.RemoteMCPServerID != "" {
		sb = sb.Where(squirrel.Eq{"remote_mcp_server_id": arg.RemoteMCPServerID})
	}
	if arg.MCPServerID != "" {
		sb = sb.Where(squirrel.Eq{"mcp_server_id": arg.MCPServerID})
	}
	if arg.MetaMCPServerID != "" {
		sb = sb.Where(squirrel.Eq{"meta_mcp_server_id": arg.MetaMCPServerID})
	}
	if arg.EventSource != "" {
		sb = sb.Where(squirrel.Eq{"event_source": arg.EventSource})
	}
	if arg.HookSource != "" {
		sb = sb.Where(squirrel.Eq{"hook_source": arg.HookSource})
	}
	// The exclusion is a default, not a veto: a caller naming a specific
	// hook_source gets that source, even a Speakeasy-hosted one.
	if len(arg.ExcludedHookSources) > 0 && arg.HookSource == "" {
		sb = sb.Where(squirrel.NotEq{"hook_source": arg.ExcludedHookSources})
	}
	sb = withAccountTypeFilter(sb, arg.AccountType)
	if arg.ExternalOrgID != "" {
		sb = sb.Where(squirrel.Eq{"external_org_id": arg.ExternalOrgID})
	}
	return sb
}

func (r overviewRow) summary() *OverviewSummary {
	return &OverviewSummary{
		TotalChats:               r.TotalChats,
		ResolvedChats:            r.ResolvedChats,
		FailedChats:              r.FailedChats,
		AvgSessionDurationMs:     r.AvgSessionDurationMs,
		AvgResolutionTimeMs:      r.AvgResolutionTimeMs,
		TotalInputTokens:         r.TotalInputTokens,
		TotalOutputTokens:        r.TotalOutputTokens,
		TotalTokens:              r.TotalTokens,
		CacheReadInputTokens:     r.CacheReadInputTokens,
		CacheCreationInputTokens: r.CacheCreationInputTokens,
		TotalCost:                r.TotalCost,
		TotalToolCalls:           r.TotalToolCalls,
		FailedToolCalls:          r.FailedToolCalls,
		AvgLatencyMs:             r.AvgToolLatencyMs,
	}
}

func (r overviewRow) bucket() TimeSeriesBucket {
	return TimeSeriesBucket{
		BucketTimeUnixNano:       r.BucketKey,
		TotalChats:               r.TotalChats,
		ResolvedChats:            r.ResolvedChats,
		FailedChats:              r.FailedChats,
		PartialChats:             r.PartialChats,
		AbandonedChats:           r.AbandonedChats,
		TotalInputTokens:         r.TotalInputTokens,
		TotalOutputTokens:        r.TotalOutputTokens,
		TotalTokens:              r.TotalTokens,
		CacheReadInputTokens:     r.CacheReadInputTokens,
		CacheCreationInputTokens: r.CacheCreationInputTokens,
		TotalCost:                r.TotalCost,
		TotalToolCalls:           r.TotalToolCalls,
		FailedToolCalls:          r.FailedToolCalls,
		AvgToolLatencyMs:         r.AvgToolLatencyMs,
		AvgSessionDurationMs:     r.AvgSessionDurationMs,
	}
}

func (r overviewRow) toolMetric() ToolMetric {
	return ToolMetric{
		GramURN:      r.ToolKey,
		CallCount:    r.TotalToolCalls,
		SuccessCount: r.SuccessToolCalls,
		FailureCount: r.FailedToolCalls,
		AvgLatencyMs: r.AvgToolLatencyMs,
		FailureRate:  float64(r.FailedToolCalls) / max(float64(r.TotalToolCalls), 1),
	}
}

// fillTimeSeries returns one bucket per interval across the range, zero where
// nothing happened, as GetTimeSeriesMetrics's WITH FILL does.
func fillTimeSeries(buckets map[int64]TimeSeriesBucket, timeStart, timeEnd, intervalSeconds int64) []TimeSeriesBucket {
	step := intervalSeconds * 1_000_000_000
	alignedStart := (timeStart / step) * step
	alignedEnd := ((timeEnd / step) * step) + step
	series := make([]TimeSeriesBucket, 0, (alignedEnd-alignedStart)/step)
	for t := alignedStart; t < alignedEnd; t += step {
		bucket, ok := buckets[t]
		if !ok {
			bucket.BucketTimeUnixNano = t
		}
		series = append(series, bucket)
	}
	return series
}

// topTools orders the tools as GetToolMetricsBreakdown does for "count" and
// "failure_rate", breaking ties by URN so the order is stable.
func topTools(tools []ToolMetric, limit int) (byCount, byFailureRate []ToolMetric) {
	byCount = append([]ToolMetric(nil), tools...)
	sort.Slice(byCount, func(i, j int) bool {
		if byCount[i].CallCount != byCount[j].CallCount {
			return byCount[i].CallCount > byCount[j].CallCount
		}
		return byCount[i].GramURN < byCount[j].GramURN
	})
	byFailureRate = append([]ToolMetric(nil), tools...)
	sort.Slice(byFailureRate, func(i, j int) bool {
		if byFailureRate[i].FailureRate != byFailureRate[j].FailureRate {
			return byFailureRate[i].FailureRate > byFailureRate[j].FailureRate
		}
		if byFailureRate[i].CallCount != byFailureRate[j].CallCount {
			return byFailureRate[i].CallCount > byFailureRate[j].CallCount
		}
		return byFailureRate[i].GramURN < byFailureRate[j].GramURN
	})
	return byCount[:min(limit, len(byCount))], byFailureRate[:min(limit, len(byFailureRate))]
}
