package productmetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
	"go.opentelemetry.io/otel/trace"
)

// InsertTimeout bounds an in-flight batch below the subscription ack deadline.
const InsertTimeout = 30 * time.Second

// Repository provides synchronous inserts and bounded tenant-scoped rollup reads.
type Repository struct {
	conn clickhouse.Conn
	now  func() time.Time
}

// NewRepository borrows a ClickHouse connection with lifecycle owned by the caller.
func NewRepository(conn clickhouse.Conn, now func() time.Time) *Repository {
	return &Repository{conn: conn, now: now}
}

// AttributeTuples converts canonical attributes into native typed tuples. The
// tuple's value remains canonical JSON, with integers represented exactly.
func AttributeTuples(canonical string) (clickhouse.ArraySet, error) {
	var attrs []EncodedAttribute
	if err := json.Unmarshal([]byte(canonical), &attrs); err != nil {
		return nil, fmt.Errorf("decode canonical attributes: %w", err)
	}
	tuples := make(clickhouse.ArraySet, 0, len(attrs))
	for _, a := range attrs {
		tuples = append(tuples, clickhouse.GroupSet{Value: []any{a.Key, a.Type, string(a.Value)}})
	}
	return tuples, nil
}

// Insert returns only after the source and incremental materialized views finish.
// Ambiguous errors can have partial effects: callers retry and counts can inflate.
func (r *Repository) Insert(ctx context.Context, rows []Contribution) error {
	ctx, cancel := context.WithTimeout(ctx, InsertTimeout)
	defer cancel()
	if len(rows) == 0 {
		return nil
	}
	if len(rows) > BatchMaxMessages {
		return fmt.Errorf("metric insert exceeds record bound")
	}
	now := r.now().UTC()
	b := squirrel.Insert("product_metric_contributions").Columns("organization_id", "project_id", "metric_name", "scope_name", "scope_version", "unit", "instrument", "description", "number_kind", "resource_attributes", "scope_attributes", "point_attributes", "contribution_id", "event_time", "observed_at", "integer_value", "floating_value")
	for _, c := range rows {
		s, err := Validate(c)
		if err != nil {
			return fmt.Errorf("validate insert: %w", err)
		}
		if c.EventTime.Before(EarliestBucket(now)) || c.EventTime.After(now.Add(MaxFutureSkew)) {
			return fmt.Errorf("event time outside retained window")
		}
		resource, err := AttributeTuples(s.Resource)
		if err != nil {
			return err
		}
		scope, err := AttributeTuples(s.Scope)
		if err != nil {
			return err
		}
		point, err := AttributeTuples(s.Point)
		if err != nil {
			return err
		}
		b = b.Values(c.Tenant.OrganizationID, c.Tenant.ProjectID, c.Definition.Name, c.Definition.ScopeName, c.Definition.ScopeVersion, c.Definition.Unit, string(c.Definition.Instrument), c.Definition.Description, c.Value.kind, resource, scope, point, c.ID, c.EventTime.UTC().Format("2006-01-02 15:04:05.999999999"), c.ObservedAt.UTC().Format("2006-01-02 15:04:05.999999999"), c.Value.integer, c.Value.floating)
	}
	query, args, err := b.ToSql()
	if err != nil {
		return fmt.Errorf("build metric insert: %w", err)
	}
	// WithAsync(false) is a driver-level fire-and-forget override, not a setting.
	// Reset inherited driver options so callers cannot bypass durable completion.
	// Cancellation and the OTel span remain attached to the context.
	ctx = clickhouse.Context(ctx, func(options *clickhouse.QueryOptions) error {
		*options = clickhouse.QueryOptions{}
		return nil
	}, clickhouse.WithSpan(trace.SpanContextFromContext(ctx)), clickhouse.WithSettings(clickhouse.Settings{"async_insert": 0, "materialized_views_ignore_errors": 0, "insert_deduplicate": 0}))
	if err := r.conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert metric contributions: %w", err)
	}
	return nil
}
