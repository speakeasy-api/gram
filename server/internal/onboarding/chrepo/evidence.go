// Package chrepo reads onboarding evidence from ClickHouse: the telemetry
// and detection rows that prove a coverage technique is delivering.
package chrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Masterminds/squirrel"
)

var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// CHTX is the subset of clickhouse.Conn the queries use.
type CHTX interface {
	Query(ctx context.Context, query string, args ...any) (driver.Rows, error)
}

// Queries runs the evidence reads.
type Queries struct {
	conn CHTX
}

// New creates a Queries backed by the connection.
func New(conn CHTX) *Queries {
	return &Queries{conn: conn}
}

// CountHookEvents counts hook events for the projects since the given time.
// An empty sources list counts events from every source.
func (q *Queries) CountHookEvents(ctx context.Context, projectIDs []string, sources []string, since time.Time) (uint64, error) {
	if len(projectIDs) == 0 {
		return 0, nil
	}
	sb := sq.Select("count() AS cnt").
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": projectIDs}).
		Where("event_source = 'hook'").
		Where("time_unix_nano > ?", since.UnixNano())
	if len(sources) > 0 {
		sb = sb.Where(squirrel.Eq{"hook_source": sources})
	}
	return q.count(ctx, sb, "hook events")
}

// CountCostRows counts telemetry rows that carry token or cost usage for the
// projects since the given time. The condition matches the usage summaries'
// definition of a usage row.
func (q *Queries) CountCostRows(ctx context.Context, projectIDs []string, since time.Time) (uint64, error) {
	if len(projectIDs) == 0 {
		return 0, nil
	}
	sb := sq.Select("count() AS cnt").
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": projectIDs}).
		Where("time_unix_nano > ?", since.UnixNano()).
		Where("(toString(attributes.gen_ai.usage.input_tokens) != '' OR toString(attributes.gen_ai.usage.output_tokens) != '' OR toString(attributes.gen_ai.usage.cost) != '')")
	return q.count(ctx, sb, "cost rows")
}

// CountGatewayTraffic counts telemetry rows dispatched through a toolset or a
// gateway for the projects since the given time.
func (q *Queries) CountGatewayTraffic(ctx context.Context, projectIDs []string, since time.Time) (uint64, error) {
	if len(projectIDs) == 0 {
		return 0, nil
	}
	sb := sq.Select("count() AS cnt").
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": projectIDs}).
		Where("time_unix_nano > ?", since.UnixNano()).
		Where("(toolset_slug != '' OR meta_mcp_server_id != '')")
	return q.count(ctx, sb, "gateway traffic")
}

// CountAIDetections counts shadow AI detections reported for the organization
// since the given time.
func (q *Queries) CountAIDetections(ctx context.Context, organizationID string, since time.Time) (uint64, error) {
	sb := sq.Select("count() AS cnt").
		From("ai_detections FINAL").
		Where("organization_id = ?", organizationID).
		Where("last_seen >= ?", since)
	return q.count(ctx, sb, "ai detections")
}

func (q *Queries) count(ctx context.Context, sb squirrel.SelectBuilder, what string) (uint64, error) {
	query, args, err := sb.ToSql()
	if err != nil {
		return 0, fmt.Errorf("build %s count query: %w", what, err)
	}
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("query %s count: %w", what, err)
	}
	defer func() { _ = rows.Close() }()

	var cnt uint64
	if rows.Next() {
		if err := rows.Scan(&cnt); err != nil {
			return 0, fmt.Errorf("scan %s count: %w", what, err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read %s count: %w", what, err)
	}
	return cnt, nil
}
