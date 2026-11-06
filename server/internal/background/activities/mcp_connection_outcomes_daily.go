package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/dailyoutcomes"
)

// MCPConnectionOutcomesDaily plans and builds days of mcp_connection_outcomes_daily.
type MCPConnectionOutcomesDaily struct {
	builder *dailyoutcomes.Builder
}

func NewMCPConnectionOutcomesDaily(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, chConn clickhouse.Conn) *MCPConnectionOutcomesDaily {
	return &MCPConnectionOutcomesDaily{builder: dailyoutcomes.NewBuilder(logger, meterProvider, db, chConn)}
}

// PlanMCPConnectionOutcomeDaysArgs carries the workflow's clock.
type PlanMCPConnectionOutcomeDaysArgs struct {
	Now time.Time
}

// BuildMCPConnectionOutcomesDayArgs names the day to build and the workflow's clock.
type BuildMCPConnectionOutcomesDayArgs struct {
	Day time.Time
	Now time.Time
}

func (m *MCPConnectionOutcomesDaily) Plan(ctx context.Context, args PlanMCPConnectionOutcomeDaysArgs) ([]time.Time, error) {
	days, err := m.builder.DaysToBuild(ctx, args.Now)
	if err != nil {
		return nil, fmt.Errorf("plan connection outcome days: %w", err)
	}
	return days, nil
}

func (m *MCPConnectionOutcomesDaily) Build(ctx context.Context, args BuildMCPConnectionOutcomesDayArgs) (*dailyoutcomes.Result, error) {
	stop := startActivityHeartbeat(ctx)
	defer stop()

	result, err := m.builder.BuildDay(ctx, args.Day, args.Now)
	if errors.Is(err, dailyoutcomes.ErrDayOpen) {
		return nil, temporal.NewNonRetryableApplicationError("connection outcome day is still open", "day_open", err)
	}
	if err != nil {
		return nil, fmt.Errorf("build connection outcome day: %w", err)
	}
	return &result, nil
}
