package productmetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// QueryMaxRows bounds returned groups without truncating aggregate results.
	QueryMaxRows = 10000
	// QueryMaxReadRows bounds scans to ten million rollup rows per request.
	QueryMaxReadRows = 10000000
	// QueryMaxReadBytes bounds uncompressed scan work to 1 GiB.
	QueryMaxReadBytes = 1 << 30
	// QueryMaxMemory bounds aggregate state memory to 256 MiB.
	QueryMaxMemory = 256 << 20
	// QueryMaxSeconds caps execution at ten seconds; exceeding it returns an error.
	QueryMaxSeconds = 10
	// QueryMaxDimensions bounds query expression size, not stored series cardinality.
	QueryMaxDimensions = 32
)

// Namespace selects an attribute namespace without interpolating SQL identifiers.
type Namespace string

const (
	// Resource selects resource attributes.
	Resource Namespace = "resource"
	// Scope selects instrumentation scope attributes.
	Scope Namespace = "scope"
	// Point selects point attributes.
	Point Namespace = "point"
)

// Dimension identifies an arbitrary producer-owned attribute.
type Dimension struct {
	// Namespace disambiguates resource, scope and point attributes.
	Namespace Namespace

	// Key is the exact attribute name, bound as a query parameter.
	Key string
}

func (d Dimension) column() (string, error) {
	if d.Key == "" {
		return "", fmt.Errorf("attribute key is required")
	}
	switch d.Namespace {
	case Resource:
		return "resource_attributes", nil
	case Scope:
		return "scope_attributes", nil
	case Point:
		return "point_attributes", nil
	default:
		return "", fmt.Errorf("unsupported attribute namespace")
	}
}

// Filter matches an exact typed scalar/array value or an absent attribute.
type Filter struct {
	// Dimension identifies the attribute namespace and key.
	Dimension Dimension

	// Value is the exact typed value when Missing is false.
	Value attribute.Value

	// Missing selects absent keys rather than the empty string/array.
	Missing bool
}

// Query scopes every read by tenant, full descriptor and a half-open UTC range.
type Query struct {
	// Tenant prevents implicit cross-tenant/project aggregation.
	Tenant Tenant

	// Definition selects one registered descriptor meaning.
	Definition Definition

	// Start is an inclusive whole-minute boundary.
	Start time.Time

	// End is an exclusive whole-minute boundary, at most the next minute.
	End time.Time

	// Interval is a whole-minute bucket width, aligned to the Unix epoch.
	Interval time.Duration

	// Filters are ANDed typed equality or missing-key predicates.
	Filters []Filter

	// GroupBy selects arbitrary retained dimensions; omitted dimensions collapse.
	GroupBy []Dimension
}

// GroupValue distinguishes missing attributes from every present typed value.
type GroupValue struct {
	// Present is false only when the attribute key is absent.
	Present bool

	// Type is the OTel value type when present.
	Type string

	// Value contains exact canonical JSON, including quoted strings.
	Value json.RawMessage
}

// Aggregate is one bucket/group and numeric representation. Integer and floating
// series remain separate rather than silently converting integers to float64.
type Aggregate struct {
	// Start is the bucket's inclusive lower boundary.
	Start time.Time

	// End is its exclusive upper boundary clipped to the query range.
	End time.Time

	// Partial reports a bucket whose query coverage is clipped or still current.
	Partial bool

	// Groups follows the Query.GroupBy order.
	Groups []GroupValue

	// NumberKind is integer or floating.
	NumberKind string

	// Count is delivered increments for Counter, observations for Histogram.
	Count uint64

	// IntegerSum preserves the Int128 aggregate exactly.
	IntegerSum *big.Int

	// FloatingSum is the IEEE-754 sum for floating series.
	FloatingSum float64

	// Min is the histogram minimum; unset for Counters.
	Min Number

	// Max is the histogram maximum; unset for Counters.
	Max Number
}

// Mean returns the delivery-weighted arithmetic mean as a rational number.
// For floating observations it represents the computed floating sum exactly.
func (a Aggregate) Mean() *big.Rat {
	if a.Count == 0 {
		return nil
	}
	var sum *big.Rat
	if a.NumberKind == "integer" {
		sum = new(big.Rat).SetInt(a.IntegerSum)
	} else {
		sum = new(big.Rat).SetFloat64(a.FloatingSum)
	}
	if sum == nil {
		return nil
	}
	return sum.Quo(sum, new(big.Rat).SetInt(new(big.Int).SetUint64(a.Count)))
}

func buildQuery(q Query, now time.Time) (string, []any, error) {
	if q.Tenant.OrganizationID == "" || q.Tenant.ProjectID == uuid.Nil {
		return "", nil, fmt.Errorf("tenant and project are required")
	}
	if _, err := NewRegistry(q.Definition); err != nil {
		return "", nil, err
	}
	if q.Interval < time.Minute || q.Interval > RollupRetention || q.Interval%time.Minute != 0 {
		return "", nil, fmt.Errorf("interval must be whole minutes within retention")
	}
	if !q.Start.Equal(q.Start.Truncate(time.Minute)) || !q.End.Equal(q.End.Truncate(time.Minute)) || !q.End.After(q.Start) || q.Start.Before(EarliestBucket(now)) || q.End.After(now.UTC().Truncate(time.Minute).Add(time.Minute)) {
		return "", nil, fmt.Errorf("range must use retained whole minutes, ending no later than the current minute end")
	}
	if len(q.Filters)+len(q.GroupBy) > QueryMaxDimensions {
		return "", nil, fmt.Errorf("query exceeds dimension expression bound")
	}
	table := "product_metric_sums_1m"
	if q.Definition.Instrument == Histogram {
		table = "product_metric_histograms_1m"
	}
	b := squirrel.Select().Column("toDateTime(intDiv(toUInt64(bucket), ?) * ?, 'UTC') AS window_start", int64(q.Interval/time.Second), int64(q.Interval/time.Second)).
		Columns("number_kind", "sum(contributions) AS total_count", "toString(sum(integer_sum)) AS total_integer", "sum(floating_sum) AS total_floating").
		From(table).Where(squirrel.Eq{"organization_id": q.Tenant.OrganizationID, "project_id": q.Tenant.ProjectID, "metric_name": q.Definition.Name, "scope_name": q.Definition.ScopeName, "scope_version": q.Definition.ScopeVersion, "unit": q.Definition.Unit}).
		Where("bucket >= ? AND bucket < ?", q.Start.UTC(), q.End.UTC()).GroupBy("window_start", "number_kind").OrderBy("window_start", "number_kind")
	if q.Definition.Instrument == Histogram {
		b = b.Columns("min(integer_min)", "max(integer_max)", "min(floating_min)", "max(floating_max)")
	}
	for _, f := range q.Filters {
		col, err := f.Dimension.column()
		if err != nil {
			return "", nil, err
		}
		if f.Missing {
			b = b.Where("NOT arrayExists(a -> a.key = ?, "+col+")", f.Dimension.Key)
			continue
		}
		canonical, err := CanonicalAttributes([]attribute.KeyValue{{Key: attribute.Key(f.Dimension.Key), Value: f.Value}})
		if err != nil {
			return "", nil, err
		}
		var attrs []EncodedAttribute
		if err := json.Unmarshal([]byte(canonical), &attrs); err != nil {
			return "", nil, fmt.Errorf("decode filter: %w", err)
		}
		b = b.Where("has("+col+", tuple(?, ?, ?))", attrs[0].Key, attrs[0].Type, string(attrs[0].Value))
	}
	for i, d := range q.GroupBy {
		col, err := d.column()
		if err != nil {
			return "", nil, err
		}
		alias := fmt.Sprintf("dimension_%d", i)
		b = b.Column("toJSONString(tuple(arrayExists(a -> a.key = ?, "+col+"), arrayFirst(a -> a.key = ?, "+col+").type, arrayFirst(a -> a.key = ?, "+col+").value)) AS "+alias, d.Key, d.Key, d.Key).GroupBy(alias).OrderBy(alias)
	}
	sql, args, err := b.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build metric query: %w", err)
	}
	return sql, args, nil
}

func boundedQueryContext(ctx context.Context) context.Context {
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_result_rows": QueryMaxRows, "max_result_bytes": 16 << 20, "result_overflow_mode": "throw",
		"max_rows_to_read": QueryMaxReadRows, "max_bytes_to_read": QueryMaxReadBytes, "read_overflow_mode": "throw",
		"max_memory_usage": QueryMaxMemory, "max_execution_time": QueryMaxSeconds, "timeout_overflow_mode": "throw",
		"max_rows_to_group_by": QueryMaxRows, "group_by_overflow_mode": "throw", "max_threads": 2,
	}))
}

// Query reads compact rollups without FINAL or per-event winners. Missing data
// yields no result rows, not automatic zeros. Exceeding a bound fails the read.
func (r *Repository) Query(ctx context.Context, q Query) ([]Aggregate, error) {
	ctx, cancel := context.WithTimeout(ctx, QueryMaxSeconds*time.Second)
	defer cancel()
	now := r.now().UTC()
	sql, args, err := buildQuery(q, now)
	if err != nil {
		return nil, err
	}
	rows, err := r.conn.Query(boundedQueryContext(ctx), sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query product metrics: %w", err)
	}
	defer o11y.NoLogDefer(rows.Close)
	result := make([]Aggregate, 0)
	for rows.Next() {
		var a Aggregate
		var integer string
		var imin, imax int64
		var fmin, fmax float64
		groups := make([]string, len(q.GroupBy))
		dest := []any{&a.Start, &a.NumberKind, &a.Count, &integer, &a.FloatingSum}
		if q.Definition.Instrument == Histogram {
			dest = append(dest, &imin, &imax, &fmin, &fmax)
		}
		for i := range groups {
			dest = append(dest, &groups[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan product metrics: %w", err)
		}
		var ok bool
		a.IntegerSum, ok = new(big.Int).SetString(integer, 10)
		if !ok || !finite(a.FloatingSum) {
			return nil, fmt.Errorf("invalid or overflowing aggregate")
		}
		if q.Definition.Instrument == Histogram {
			if a.NumberKind == "integer" {
				a.Min, a.Max = Integer(imin), Integer(imax)
			} else {
				a.Min, a.Max = Float(fmin), Float(fmax)
			}
		}
		a.End = a.Start.Add(q.Interval)
		a.Partial = a.Start.Before(q.Start) || a.End.After(q.End) || a.End.After(now)
		if a.Start.Before(q.Start) {
			a.Start = q.Start
		}
		if a.End.After(q.End) {
			a.End = q.End
		}
		for _, raw := range groups {
			var tuple []json.RawMessage
			if err := json.Unmarshal([]byte(raw), &tuple); err != nil || len(tuple) != 3 {
				return nil, fmt.Errorf("invalid grouping tuple")
			}
			var present int
			var typ, value string
			if err := json.Unmarshal(tuple[0], &present); err != nil {
				return nil, fmt.Errorf("decode grouping presence: %w", err)
			}
			if err := json.Unmarshal(tuple[1], &typ); err != nil {
				return nil, fmt.Errorf("decode grouping type: %w", err)
			}
			if err := json.Unmarshal(tuple[2], &value); err != nil {
				return nil, fmt.Errorf("decode grouping value: %w", err)
			}
			a.Groups = append(a.Groups, GroupValue{Present: present != 0, Type: typ, Value: json.RawMessage(value)})
		}
		result = append(result, a)
		if len(result) > QueryMaxRows {
			return nil, fmt.Errorf("metric result exceeds row bound")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read product metrics: %w", err)
	}
	return result, nil
}
