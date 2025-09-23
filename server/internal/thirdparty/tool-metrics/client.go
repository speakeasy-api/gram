package tool_metrics

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"
)

type ToolMetricsClient interface {
	Ping(ctx context.Context) error
	Exec(ctx context.Context, query string, args ...any) error
	Close() error
}

type StubToolMetricsClient struct{}

func (n *StubToolMetricsClient) Exec(ctx context.Context, query string, args ...any) error {
	return nil
}

func (n *StubToolMetricsClient) Ping(ctx context.Context) error {
	return nil
}

func (n *StubToolMetricsClient) Stats(ctx context.Context) (map[string]interface{}, error) {
	return nil, nil
}

func (n *StubToolMetricsClient) Close() error {
	return nil
}

type ClickhouseClient struct {
	Conn   clickhouse.Conn
	Logger *slog.Logger
}

func (c *ClickhouseClient) Exec(ctx context.Context, query string, args ...any) error {
	err := c.Conn.Exec(ctx, query, args...)
	return fmt.Errorf("exec query: %w", err)
}

func (c *ClickhouseClient) Ping(ctx context.Context) error {
	err := c.Conn.Ping(ctx)
	return fmt.Errorf("ping: %w", err)
}

func (c *ClickhouseClient) Close() error {
	err := c.Conn.Close()
	return fmt.Errorf("close: %w", err)
}
