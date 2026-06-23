package repo

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Masterminds/squirrel"
)

// chatTurnMeasureAliasPrefix prefixes SELECT aliases for the computed scalar
// measures so ORDER BY never resolves to an underlying aggregate column.
const chatTurnMeasureAliasPrefix = "m_"

var chatTurnMeasureSelects = []string{
	"sum(request_count) AS m_request_count",
	"sum(input_tokens) AS m_input_tokens",
	"sum(output_tokens) AS m_output_tokens",
	"sum(total_tokens) AS m_total_tokens",
	"sum(cache_read_tokens) AS m_cache_read_tokens",
	"sum(cache_creation_tokens) AS m_cache_creation_tokens",
	"sum(cost_usd) AS m_total_cost",
	"sum(cost_usd_micros) AS m_cost_usd_micros",
	"uniqExact(tuple(chat_id, turn_id)) AS m_total_turns",
	"uniqExact(chat_id) AS m_total_chats",
}

var chatTurnMeasureSet = map[string]bool{
	"request_count":         true,
	"input_tokens":          true,
	"output_tokens":         true,
	"total_tokens":          true,
	"cache_read_tokens":     true,
	"cache_creation_tokens": true,
	"total_cost":            true,
	"cost_usd_micros":       true,
	"total_turns":           true,
	"total_chats":           true,
}

type ChatTurnSummaryMeasures struct {
	RequestCount        uint64
	InputTokens         int64
	OutputTokens        int64
	TotalTokens         int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalCost           float64
	CostUSDMicros       int64
	TotalTurns          uint64
	TotalChats          uint64
}

func (m *ChatTurnSummaryMeasures) Add(o ChatTurnSummaryMeasures) {
	m.RequestCount += o.RequestCount
	m.InputTokens += o.InputTokens
	m.OutputTokens += o.OutputTokens
	m.TotalTokens += o.TotalTokens
	m.CacheReadTokens += o.CacheReadTokens
	m.CacheCreationTokens += o.CacheCreationTokens
	m.TotalCost += o.TotalCost
	m.CostUSDMicros += o.CostUSDMicros
	m.TotalTurns += o.TotalTurns
	m.TotalChats += o.TotalChats
}

type ChatTurnSummaryRow struct {
	GroupValue          string              `ch:"group_value"`
	RequestCount        uint64              `ch:"m_request_count"`
	InputTokens         int64               `ch:"m_input_tokens"`
	OutputTokens        int64               `ch:"m_output_tokens"`
	TotalTokens         int64               `ch:"m_total_tokens"`
	CacheReadTokens     int64               `ch:"m_cache_read_tokens"`
	CacheCreationTokens int64               `ch:"m_cache_creation_tokens"`
	TotalCost           float64             `ch:"m_total_cost"`
	CostUSDMicros       int64               `ch:"m_cost_usd_micros"`
	TotalTurns          uint64              `ch:"m_total_turns"`
	TotalChats          uint64              `ch:"m_total_chats"`
	DimensionValues     map[string][]string `ch:"dimension_values"`
}

func (r ChatTurnSummaryRow) Measures() ChatTurnSummaryMeasures {
	return ChatTurnSummaryMeasures{
		RequestCount:        r.RequestCount,
		InputTokens:         r.InputTokens,
		OutputTokens:        r.OutputTokens,
		TotalTokens:         r.TotalTokens,
		CacheReadTokens:     r.CacheReadTokens,
		CacheCreationTokens: r.CacheCreationTokens,
		TotalCost:           r.TotalCost,
		CostUSDMicros:       r.CostUSDMicros,
		TotalTurns:          r.TotalTurns,
		TotalChats:          r.TotalChats,
	}
}

type ChatTurnSummaryTimePoint struct {
	GroupValue          string  `ch:"group_value"`
	BucketTimeUnixNano  int64   `ch:"bucket_time_unix_nano"`
	RequestCount        uint64  `ch:"m_request_count"`
	InputTokens         int64   `ch:"m_input_tokens"`
	OutputTokens        int64   `ch:"m_output_tokens"`
	TotalTokens         int64   `ch:"m_total_tokens"`
	CacheReadTokens     int64   `ch:"m_cache_read_tokens"`
	CacheCreationTokens int64   `ch:"m_cache_creation_tokens"`
	TotalCost           float64 `ch:"m_total_cost"`
	CostUSDMicros       int64   `ch:"m_cost_usd_micros"`
	TotalTurns          uint64  `ch:"m_total_turns"`
	TotalChats          uint64  `ch:"m_total_chats"`
}

func (p ChatTurnSummaryTimePoint) Measures() ChatTurnSummaryMeasures {
	return ChatTurnSummaryMeasures{
		RequestCount:        p.RequestCount,
		InputTokens:         p.InputTokens,
		OutputTokens:        p.OutputTokens,
		TotalTokens:         p.TotalTokens,
		CacheReadTokens:     p.CacheReadTokens,
		CacheCreationTokens: p.CacheCreationTokens,
		TotalCost:           p.TotalCost,
		CostUSDMicros:       p.CostUSDMicros,
		TotalTurns:          p.TotalTurns,
		TotalChats:          p.TotalChats,
	}
}

type ChatTurnSummaryFilter struct {
	Dimension string
	Values    []string
}

type ChatTurnSummaryQueryParams struct {
	ProjectIDs      []string
	TimeStart       int64
	TimeEnd         int64
	GroupBy         string
	SortBy          string
	Filters         []ChatTurnSummaryFilter
	IntervalSeconds int64
}

func chatTurnGroupValueExpr(groupBy string) (expr string, grouped bool, err error) {
	if groupBy == "" {
		return "''", false, nil
	}
	dim, ok := chatTurnDimensionRegistry[groupBy]
	if !ok {
		return "", false, fmt.Errorf("unknown group_by dimension %q", groupBy)
	}
	switch dim.kind {
	case attributeDimArray:
		return "arrayJoin(if(empty(" + dim.column + "), [''], " + dim.column + "))", true, nil
	case attributeDimProject:
		return "toString(" + dim.column + ")", true, nil
	case attributeDimScalar:
		return dim.column, true, nil
	default:
		return "", false, fmt.Errorf("unhandled dimension kind for %q", groupBy)
	}
}

func chatTurnDimensionValuesExpr(groupBy string) string {
	keys := make([]string, 0, len(chatTurnDimensionRegistry))
	for k := range chatTurnDimensionRegistry {
		if k == groupBy {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	capStr := strconv.Itoa(maxDimensionValues)
	parts := make([]string, 0, len(keys)*2)
	for _, k := range keys {
		dim := chatTurnDimensionRegistry[k]
		var collected string
		switch dim.kind {
		case attributeDimArray:
			collected = "groupUniqArrayArray(" + capStr + ")(" + dim.column + ")"
		case attributeDimProject:
			collected = "groupUniqArray(" + capStr + ")(toString(" + dim.column + "))"
		case attributeDimScalar:
			collected = "groupUniqArray(" + capStr + ")(" + dim.column + ")"
		}
		valExpr := "arrayFilter(x -> x != '', " + collected + ")"
		parts = append(parts, "'"+k+"', "+valExpr)
	}
	return "map(" + strings.Join(parts, ", ") + ") AS dimension_values"
}

func applyChatTurnFilters(sb squirrel.SelectBuilder, filters []ChatTurnSummaryFilter) (squirrel.SelectBuilder, error) {
	for _, f := range filters {
		if len(f.Values) == 0 {
			continue
		}
		dim, ok := chatTurnDimensionRegistry[f.Dimension]
		if !ok {
			return sb, fmt.Errorf("unknown filter dimension %q", f.Dimension)
		}
		switch dim.kind {
		case attributeDimArray:
			sb = sb.Where(arrayDimFilter(dim.column, f.Values))
		case attributeDimScalar, attributeDimProject:
			sb = sb.Where(squirrel.Eq{dim.column: f.Values})
		default:
			return sb, fmt.Errorf("unhandled dimension kind for filter %q", f.Dimension)
		}
	}
	return sb, nil
}

// QueryChatTurnSummariesTable returns one row per group value (or a single row
// when GroupBy is empty), aggregated over the whole time range and ordered by
// SortBy descending.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) QueryChatTurnSummariesTable(ctx context.Context, arg ChatTurnSummaryQueryParams) ([]ChatTurnSummaryRow, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}

	groupExpr, grouped, err := chatTurnGroupValueExpr(arg.GroupBy)
	if err != nil {
		return nil, err
	}
	if !chatTurnMeasureSet[arg.SortBy] {
		return nil, fmt.Errorf("unknown sort_by measure %q", arg.SortBy)
	}

	sb := sq.Select(groupExpr+" AS group_value").
		Columns(chatTurnMeasureSelects...).
		Column(squirrel.Expr(chatTurnDimensionValuesExpr(arg.GroupBy))).
		From("chat_turn_summaries").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("start_time_unix_nano >= ?", arg.TimeStart).
		Where("start_time_unix_nano <= ?", arg.TimeEnd)

	sb, err = applyChatTurnFilters(sb, arg.Filters)
	if err != nil {
		return nil, err
	}

	if grouped {
		sb = sb.GroupBy("group_value")
	}
	sb = sb.OrderBy(chatTurnMeasureAliasPrefix + arg.SortBy + " DESC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building chat turn summaries table query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChatTurnSummaryRow
	for rows.Next() {
		var row ChatTurnSummaryRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning chat turn summary row: %w", err)
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// QueryChatTurnSummariesTimeseries returns raw (group, bucket) cells aggregated
// at IntervalSeconds. Buckets are not gap-filled and groups are not limited; the
// service layer handles "Other" rollup and zero-fill.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) QueryChatTurnSummariesTimeseries(ctx context.Context, arg ChatTurnSummaryQueryParams) ([]ChatTurnSummaryTimePoint, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}

	groupExpr, grouped, err := chatTurnGroupValueExpr(arg.GroupBy)
	if err != nil {
		return nil, err
	}

	sb := sq.Select().
		Column(squirrel.Expr(
			"toInt64(toStartOfInterval(fromUnixTimestamp64Nano(start_time_unix_nano), toIntervalSecond(?))) * 1000000000 AS bucket_time_unix_nano",
			arg.IntervalSeconds,
		)).
		Column(groupExpr+" AS group_value").
		Columns(chatTurnMeasureSelects...).
		From("chat_turn_summaries").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("start_time_unix_nano >= ?", arg.TimeStart).
		Where("start_time_unix_nano <= ?", arg.TimeEnd)

	sb, err = applyChatTurnFilters(sb, arg.Filters)
	if err != nil {
		return nil, err
	}

	groupCols := []string{"bucket_time_unix_nano"}
	if grouped {
		groupCols = append(groupCols, "group_value")
	}
	sb = sb.GroupBy(groupCols...)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building chat turn summaries timeseries query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChatTurnSummaryTimePoint
	for rows.Next() {
		var point ChatTurnSummaryTimePoint
		if err = rows.ScanStruct(&point); err != nil {
			return nil, fmt.Errorf("scanning chat turn summary timeseries row: %w", err)
		}
		out = append(out, point)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
