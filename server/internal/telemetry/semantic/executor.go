package semantic

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Conn is the minimal ClickHouse connection surface the executor needs;
// clickhouse.Conn satisfies it.
type Conn interface {
	Query(ctx context.Context, query string, args ...any) (driver.Rows, error)
}

// MeasureValue is one aggregated measure value, typed per the measure's
// declared scan type. Exactly one of the value fields is populated; Type
// names which (float64 | int64 | uint64).
type MeasureValue struct {
	Type    string
	Float64 float64
	Int64   int64
	Uint64  uint64
}

// Row is one result row of an executed plan. Table rows carry GroupValue
// (and DimensionValues when requested); timeseries rows additionally carry
// BucketTimeUnixNano.
type Row struct {
	GroupValue         string
	BucketTimeUnixNano int64
	Measures           map[string]MeasureValue
	DimensionValues    map[string][]string
}

// Execute compiles the plan and runs it, scanning rows generically per the
// plan's measure scan types.
func Execute(ctx context.Context, conn Conn, plan *QueryPlan) ([]Row, error) {
	query, args, err := Compile(plan)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executing semantic query: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	timeseries := plan.Query.GranularitySeconds > 0
	var out []Row
	for rows.Next() {
		row, err := scanRow(rows, plan, timeseries)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading semantic query rows: %w", err)
	}
	return out, nil
}

// scanRow builds Scan destinations in SELECT order — bucket (timeseries
// only), group_value, one typed pointer per measure, dimension_values when
// included — and folds them into a Row.
func scanRow(rows driver.Rows, plan *QueryPlan, timeseries bool) (Row, error) {
	var (
		bucket     int64
		groupValue string
		dimValues  map[string][]string
	)
	floats := make([]float64, len(plan.Measures))
	ints := make([]int64, len(plan.Measures))
	uints := make([]uint64, len(plan.Measures))

	dests := make([]any, 0, len(plan.Measures)+3)
	if timeseries {
		dests = append(dests, &bucket)
	}
	dests = append(dests, &groupValue)
	for i, ms := range plan.Measures {
		switch ms.Type {
		case MeasureTypeFloat64:
			dests = append(dests, &floats[i])
		case MeasureTypeInt64:
			dests = append(dests, &ints[i])
		case MeasureTypeUint64:
			dests = append(dests, &uints[i])
		default:
			return Row{}, fmt.Errorf("unhandled measure scan type %q for %q", ms.Type, ms.Name)
		}
	}
	if !timeseries && len(plan.DimensionValuesDims) > 0 {
		dests = append(dests, &dimValues)
	}

	if err := rows.Scan(dests...); err != nil {
		return Row{}, fmt.Errorf("scanning semantic query row: %w", err)
	}

	measures := make(map[string]MeasureValue, len(plan.Measures))
	for i, ms := range plan.Measures {
		mv := MeasureValue{Type: ms.Type, Float64: 0, Int64: 0, Uint64: 0}
		switch ms.Type {
		case MeasureTypeFloat64:
			mv.Float64 = floats[i]
		case MeasureTypeInt64:
			mv.Int64 = ints[i]
		case MeasureTypeUint64:
			mv.Uint64 = uints[i]
		}
		measures[ms.Name] = mv
	}

	return Row{
		GroupValue:         groupValue,
		BucketTimeUnixNano: bucket,
		Measures:           measures,
		DimensionValues:    dimValues,
	}, nil
}
