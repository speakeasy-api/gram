package repo

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// acknowledgedInsertContext pins an acknowledged async insert: ClickHouse
// buffers the rows, but the call returns only once they are committed, so the
// next read sees them. Every insert must state its posture: one that sends no
// async settings inherits ClickHouse's default async_insert=1 (since 26.2)
// together with the server connection's wait_for_async_insert=0, which makes
// it fire-and-forget.
//
// Do not use it for tables with CHECK constraints whose violations the caller
// reports per insert: the async queue flushes rows from many queries as one
// block, so one writer's bad row fails every insert in that flush. Use
// synchronousInsertContext there.
func acknowledgedInsertContext(ctx context.Context) context.Context {
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"async_insert":          1,
		"wait_for_async_insert": 1,
	}))
}

// synchronousInsertContext pins a synchronous insert (async_insert=0): the rows
// are written as their own block and committed before the call returns, so a
// constraint violation fails only this insert.
func synchronousInsertContext(ctx context.Context) context.Context {
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"async_insert": 0,
	}))
}
