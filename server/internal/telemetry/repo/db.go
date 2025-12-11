package repo

import (
	"context"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"go.opentelemetry.io/otel/trace"
)

// CHTX is the interface for executing ClickHouse queries and commands.
// It matches the subset of methods we use from clickhouse.Conn.
type CHTX interface {
	Exec(ctx context.Context, query string, args ...interface{}) error
	Query(ctx context.Context, query string, args ...interface{}) (driver.Rows, error)
}

// Queries contains methods for executing database operations.
type Queries struct {
	conn       CHTX
	logger     *slog.Logger
	tracer     trace.Tracer
	ShouldFlag func(ctx context.Context, orgId string) (bool, error)
}

// WithConn returns a new Queries instance using the provided connection.
func (q *Queries) WithConn(conn CHTX) *Queries {
	return &Queries{
		conn:       conn,
		logger:     q.logger,
		tracer:     q.tracer,
		ShouldFlag: q.ShouldFlag,
	}
}

// New creates a new Queries instance with logger and tracer.
func New(logger *slog.Logger, traceProvider trace.TracerProvider, conn CHTX, shouldFlag func(ctx context.Context, orgId string) (bool, error)) *Queries {
	if shouldFlag == nil {
		shouldFlag = func(ctx context.Context, orgId string) (bool, error) {
			return true, nil
		}
	}

	tracer := traceProvider.Tracer("github.com/speakeasy-api/gram/server/internal/telemetry/repo")

	return &Queries{
		conn:       conn,
		logger:     logger,
		tracer:     tracer,
		ShouldFlag: shouldFlag,
	}
}
