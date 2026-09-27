package analytics

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Querier is the subset of clickhouse.Conn a plan needs to run.
type Querier interface {
	Query(ctx context.Context, query string, args ...any) (driver.Rows, error)
}

// Row is one result row: dimension names and measure aliases to values.
// Times are RFC 3339 strings in UTC, so a client parses one thing.
type Row map[string]any

// Run executes the plan and returns its rows.
func (p *Plan) Run(ctx context.Context, conn Querier) ([]Row, error) {
	rows, err := conn.Query(ctx, p.SQL, p.Args...)
	if err != nil {
		return nil, fmt.Errorf("run analytics query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns := rows.Columns()
	types := rows.ColumnTypes()
	if len(columns) != len(types) {
		return nil, fmt.Errorf("run analytics query: %d columns but %d column types", len(columns), len(types))
	}

	out := make([]Row, 0, 64)
	for rows.Next() {
		dest := make([]any, len(columns))
		for i, ct := range types {
			dest[i] = reflect.New(ct.ScanType()).Interface()
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan analytics row: %w", err)
		}
		row := make(Row, len(columns))
		for i, name := range columns {
			row[name] = resultValue(reflect.ValueOf(dest[i]).Elem().Interface())
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read analytics rows: %w", err)
	}
	return out, nil
}

// resultValue renders a scanned value for the wire. Times become RFC 3339
// in UTC at their full precision, so an event time keeps its nanoseconds;
// unsigned counts become int64 so JSON clients see one integer kind;
// everything else passes through.
func resultValue(value any) any {
	switch v := value.(type) {
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case *time.Time:
		if v == nil {
			return nil
		}
		return v.UTC().Format(time.RFC3339Nano)
	case uint64:
		if v > uint64(1<<63-1) {
			return v
		}
		return int64(v)
	case uint32:
		return int64(v)
	case uint16:
		return int64(v)
	case uint8:
		return int64(v)
	case int32:
		return int64(v)
	case int16:
		return int64(v)
	case int8:
		return int64(v)
	case float32:
		return float64(v)
	}
	return value
}
