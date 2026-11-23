package analytics

import (
	"context"
	"fmt"
	"slices"

	gen "github.com/speakeasy-api/gram/server/gen/analytics"
	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
)

// Dimension value listing guardrails.
const (
	DefaultValuesLimit = 50
	MaxValuesLimit     = 200
)

// ValuesRequest asks for the values a dimension holds inside a window.
type ValuesRequest struct {
	Dataset      string
	Dimension    string
	FromUnixNano int64
	ToUnixNano   int64
	Limit        int
	// SignalScope is required for signal attribute discovery.
	SignalScope *signalquery.Scope
	// Attribute is an exact label key when Dimension is attribute.
	Attribute string
}

// DimensionValue is one value and how many rows at the dataset's grain
// carry it.
type DimensionValue struct {
	Value string
	Count int64
	// Present distinguishes missing signal labels from present empty values.
	Present *bool
}

// CompileValues turns a values request into SQL. A dimension with a Typeahead
// column is listed from the dataset's observations and counted at its grain,
// so the listing never pays for the redelivery collapse. Any other dimension
// is listed from the folded rows. The dimension is read through its lookup,
// so the picker offers what a filter will match, and the empty value is never
// offered.
func CompileValues(catalog *Catalog, tenant Tenant, lookups LookupMaps, req ValuesRequest) (*Plan, error) {
	ds, ok := catalog.Dataset(req.Dataset)
	if !ok {
		return nil, newError(ErrUnknownDataset, req.Dataset, "dataset", req.Dataset, fmt.Sprintf("dataset %q does not exist", req.Dataset))
	}
	field, ok := ds.Field(req.Dimension)
	if !ok || field.Role != RoleDimension {
		return nil, newError(ErrUnknownField, ds.Name, "dimension", req.Dimension, fmt.Sprintf("dataset %s has no dimension %s", ds.Name, req.Dimension))
	}
	if req.ToUnixNano <= req.FromUnixNano {
		return nil, newError(ErrInvalidTimeRange, ds.Name, "to", "", "to must be after from")
	}
	if spanExceeds(req.FromUnixNano, req.ToUnixNano, ds.MaxTimeRangeNanos()) {
		return nil, newError(ErrInvalidTimeRange, ds.Name, "to", "", fmt.Sprintf("time range exceeds %d days", ds.MaxTimeRangeDays()))
	}
	limit := req.Limit
	if limit == 0 {
		limit = DefaultValuesLimit
	}
	if limit < 0 || limit > MaxValuesLimit {
		return nil, newError(ErrLimitExceeded, ds.Name, "limit", fmt.Sprint(req.Limit), fmt.Sprintf("limit must be between 1 and %d", MaxValuesLimit))
	}

	qc := QueryContext{Tenant: tenant, Window: Window{FromUnixNano: req.FromUnixNano, ToUnixNano: req.ToUnixNano}, Lookups: lookups}
	source, count := ds.Source.Rows(qc), "count()"
	if field.Typeahead != "" {
		typeahead := *field
		typeahead.Expr = field.Typeahead
		field = &typeahead
		source, count = ds.Source.Observations(qc), "uniqExact("+ds.Source.Key+")"
	}
	expr, args := readExpr(qc, field)
	builder := sq.Select().
		Column(expr+" AS value", args...).
		Column(count+" AS n").
		FromSelect(source, "src").
		Where(expr+" <> ?", append(slices.Clone(args), "")...).
		GroupBy("value").
		OrderBy("n DESC", "value ASC").
		Limit(uint64(limit))

	plan := &Plan{
		Dataset: ds.Name,
		Name:    ds.Name + "." + ds.Source.Table.planSuffix(),
		SQL:     "",
		Args:    nil,
		Columns: []Column{{Name: "value", Kind: ColumnDimension}, {Name: "n", Kind: ColumnMeasure}},
	}
	return finish(plan, builder)
}

// RunValues executes a values plan.
func (p *Plan) RunValues(ctx context.Context, conn Querier) ([]DimensionValue, error) {
	rows, err := conn.Query(ctx, p.SQL, p.Args...)
	if err != nil {
		return nil, fmt.Errorf("run dimension values query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]DimensionValue, 0, DefaultValuesLimit)
	for rows.Next() {
		var value string
		var count uint64
		if err := rows.Scan(&value, &count); err != nil {
			return nil, fmt.Errorf("scan dimension value: %w", err)
		}
		out = append(out, DimensionValue{Value: value, Count: int64(min(count, uint64(1<<63-1))), Present: nil})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read dimension values: %w", err)
	}
	return out, nil
}

// DimensionValues lists the values a dimension holds inside a window, most
// frequent first, for filter pickers.
func (s *Service) DimensionValues(ctx context.Context, payload *gen.DimensionValuesPayload) (*gen.AnalyticsDimensionValuesResult, error) {
	authCtx, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeDataset(ctx, authCtx, payload.Dataset); err != nil {
		return nil, err
	}
	if payload.Dataset == signalquery.Dataset {
		if err := s.requireSignals(ctx, authCtx.ActiveOrganizationID); err != nil {
			return nil, err
		}
	}

	from, to, err := ParseWindow(payload.From, payload.To)
	if err != nil {
		return nil, s.failure(ctx, err, "failed to read dimension values request")
	}
	req := ValuesRequest{
		Dataset:      payload.Dataset,
		Dimension:    payload.Dimension,
		FromUnixNano: from,
		ToUnixNano:   to,
		Limit:        payload.Limit,
		SignalScope:  nil,
		Attribute:    deref(payload.Attribute),
	}
	if payload.SignalScope != nil {
		req.SignalScope = &signalquery.Scope{SensorID: payload.SignalScope.SensorID, Trigger: payload.SignalScope.Trigger, RunID: deref(payload.SignalScope.RunID)}
	}

	tenant := Tenant{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: authCtx.ProjectID.String()}
	values, err := s.engine.Values(ctx, tenant, req)
	if err != nil {
		return nil, s.failure(ctx, err, "failed to list dimension values")
	}

	out := make([]*gen.AnalyticsDimensionValue, 0, len(values))
	for _, v := range values {
		out = append(out, &gen.AnalyticsDimensionValue{Value: v.Value, Count: v.Count, Present: v.Present})
	}
	return &gen.AnalyticsDimensionValuesResult{Dataset: req.Dataset, Dimension: req.Dimension, Values: out}, nil
}
