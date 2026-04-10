package observability

import (
	"context"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// SearchLog represents a single corpus search event.
type SearchLog struct {
	ID          string  `ch:"id"`
	ProjectID   string  `ch:"project_id"`
	Query       string  `ch:"query"`
	Filters     string  `ch:"filters"`
	ResultCount uint32  `ch:"result_count"`
	LatencyMs   float64 `ch:"latency_ms"`
	SessionID   string  `ch:"session_id"`
	Agent       string  `ch:"agent"`
	Timestamp   int64   `ch:"timestamp"`
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

// SearchLogs returns paginated search event logs for a project.
func (s *Service) SearchLogs(_ context.Context, _ string, _ int, _ string) (*SearchLogsResult, error) {
	return nil, nil
}

// SearchStats returns aggregated search statistics for a project.
func (s *Service) SearchStats(_ context.Context, _ string) (*SearchStatsResult, error) {
	return nil, nil
}
