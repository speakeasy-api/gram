package observability

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// sq is the squirrel statement builder pre-configured for ClickHouse (uses ? placeholders).
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// SearchLog represents a single corpus search event.
type SearchLog struct {
	ID          string    `ch:"id"`
	ProjectID   string    `ch:"project_id"`
	Query       string    `ch:"query"`
	Filters     string    `ch:"filters"`
	ResultCount uint32    `ch:"result_count"`
	LatencyMs   float64   `ch:"latency_ms"`
	SessionID   string    `ch:"session_id"`
	Agent       string    `ch:"agent"`
	Timestamp   time.Time `ch:"timestamp"`
}

// SearchLogsResult is the paginated response for search logs.
type SearchLogsResult struct {
	Logs       []SearchLog
	NextCursor string
}

// QueryFrequency represents a query string and how often it was searched.
type QueryFrequency struct {
	Query string
	Count uint64
}

// SearchStatsResult contains aggregated search statistics.
type SearchStatsResult struct {
	TopQueries  []QueryFrequency
	LatencyP50  float64
	LatencyP95  float64
	LatencyP99  float64
	TotalEvents uint64
}

// Service provides corpus search observability queries against ClickHouse.
type Service struct {
	logger *slog.Logger
	chConn clickhouse.Conn
}

// NewService creates a new observability service.
func NewService(logger *slog.Logger, chConn clickhouse.Conn) *Service {
	return &Service{
		logger: logger.With(attr.SlogComponent("corpus-observability")),
		chConn: chConn,
	}
}

// encodeCursor encodes an offset as a base64 cursor string.
func encodeCursor(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

// decodeCursor decodes a base64 cursor string into an offset. Returns 0 for empty cursors.
func decodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("decode cursor: %w", err)
	}
	offset, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, fmt.Errorf("parse cursor offset: %w", err)
	}
	return offset, nil
}

// SearchLogs returns paginated search event logs for a project.
func (s *Service) SearchLogs(ctx context.Context, projectID string, limit int, cursor string) (*SearchLogsResult, error) {
	offset, err := decodeCursor(cursor)
	if err != nil {
		return nil, fmt.Errorf("search logs: %w", err)
	}

	if limit <= 0 {
		limit = 20
	}

	// Query limit+1 to detect whether there is a next page.
	//nolint:gosec // limit and offset are validated non-negative above
	query, args, err := sq.Select(
		"id", "project_id", "query", "filters",
		"result_count", "latency_ms", "session_id", "agent", "timestamp",
	).
		From("corpus_search_events").
		Where(squirrel.Eq{"project_id": projectID}).
		OrderBy("timestamp DESC", "id DESC").
		Limit(uint64(limit + 1)).
		Offset(uint64(offset)).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build search logs query: %w", err)
	}

	rows, err := s.chConn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query search logs: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	var logs []SearchLog
	for rows.Next() {
		var l SearchLog
		if err := rows.ScanStruct(&l); err != nil {
			return nil, fmt.Errorf("scan search log: %w", err)
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search logs: %w", err)
	}

	var nextCursor string
	if len(logs) > limit {
		logs = logs[:limit]
		nextCursor = encodeCursor(offset + limit)
	}

	if logs == nil {
		logs = []SearchLog{}
	}

	return &SearchLogsResult{
		Logs:       logs,
		NextCursor: nextCursor,
	}, nil
}

// SearchStats returns aggregated search statistics for a project.
func (s *Service) SearchStats(ctx context.Context, projectID string) (*SearchStatsResult, error) {
	// Top queries by frequency.
	topQ, topArgs, err := sq.Select("query", "count(*) AS cnt").
		From("corpus_search_events").
		Where(squirrel.Eq{"project_id": projectID}).
		GroupBy("query").
		OrderBy("cnt DESC").
		Limit(50).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build top queries query: %w", err)
	}

	topRows, err := s.chConn.Query(ctx, topQ, topArgs...)
	if err != nil {
		return nil, fmt.Errorf("query top queries: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return topRows.Close() })

	var topQueries []QueryFrequency
	for topRows.Next() {
		var qf QueryFrequency
		if err := topRows.Scan(&qf.Query, &qf.Count); err != nil {
			return nil, fmt.Errorf("scan top query: %w", err)
		}
		topQueries = append(topQueries, qf)
	}
	if err := topRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top queries: %w", err)
	}

	// Latency percentiles and total count.
	percQ, percArgs, err := sq.Select(
		"quantile(0.50)(latency_ms) AS p50",
		"quantile(0.95)(latency_ms) AS p95",
		"quantile(0.99)(latency_ms) AS p99",
		"count(*) AS total",
	).
		From("corpus_search_events").
		Where(squirrel.Eq{"project_id": projectID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build percentiles query: %w", err)
	}

	var latencyP50, latencyP95, latencyP99 float64
	var totalEvents uint64

	row := s.chConn.QueryRow(ctx, percQ, percArgs...)
	if err := row.Scan(&latencyP50, &latencyP95, &latencyP99, &totalEvents); err != nil {
		return nil, fmt.Errorf("scan percentiles: %w", err)
	}

	return &SearchStatsResult{
		TopQueries:  topQueries,
		LatencyP50:  latencyP50,
		LatencyP95:  latencyP95,
		LatencyP99:  latencyP99,
		TotalEvents: totalEvents,
	}, nil
}
