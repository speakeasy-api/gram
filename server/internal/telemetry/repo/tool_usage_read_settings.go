package repo

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// toolUsageFanOutLimit caps how many tool usage reads one request runs at once.
const toolUsageFanOutLimit = 3

// toolUsageReadContext bounds the memory of one tool usage read. Each read
// groups every trace in the window by trace_id, which over a large project's
// 90 days outgrows the server's total memory limit when a page fires several
// at once. Past the spill threshold the GROUP BY and ORDER BY go to disk, and a
// read that still exceeds the cap fails alone instead of starving every other
// query on the server.
func toolUsageReadContext(ctx context.Context) context.Context {
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_memory_usage":                   1536 << 20,
		"max_bytes_before_external_group_by": 512 << 20,
		"max_bytes_before_external_sort":     512 << 20,
	}))
}
