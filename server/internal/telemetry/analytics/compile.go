package analytics

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Masterminds/squirrel"
)

// ErrorCode names what failed in a request, so a client can correct it
// without guessing.
type ErrorCode string

const (
	ErrUnknownDataset         ErrorCode = "unknown_dataset"
	ErrUnknownField           ErrorCode = "unknown_field"
	ErrUnsupportedOperator    ErrorCode = "unsupported_operator"
	ErrUnsupportedAggregation ErrorCode = "unsupported_aggregation"
	ErrUnsupportedGrain       ErrorCode = "unsupported_grain"
	ErrInvalidTimeRange       ErrorCode = "invalid_time_range"
	ErrLimitExceeded          ErrorCode = "limit_exceeded"
	ErrTooManyDimensions      ErrorCode = "too_many_dimensions"
	ErrUnsatisfiable          ErrorCode = "unsatisfiable"
)

// Error is a request the compiler rejected. Field is the position in the
// request that failed (dimensions[1], measures[0].field, ...) and Value what
// was there.
type Error struct {
	Code    ErrorCode
	Dataset string
	Field   string
	Value   string
	Message string
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(string(e.Code))
	b.WriteString(": ")
	b.WriteString(e.Message)
	if e.Field != "" {
		fmt.Fprintf(&b, " (%s", e.Field)
		if e.Value != "" {
			fmt.Fprintf(&b, " = %q", e.Value)
		}
		b.WriteString(")")
	}
	return b.String()
}

func newError(code ErrorCode, dataset, field, value, message string) *Error {
	return &Error{Code: code, Dataset: dataset, Field: field, Value: value, Message: message}
}

// Measure is an op over a field, or count alone.
type Measure struct {
	Op    string
	Field string
	Alias string
}

// Filter is a predicate on a dimension.
type Filter struct {
	Field    string
	Operator string
	Values   []string
}

// OrderBy sorts a grouped result by a measure alias.
type OrderBy struct {
	Measure   string
	Direction string
}

// Request is a query against one dataset, in catalog vocabulary.
type Request struct {
	Dataset      string
	FromUnixNano int64
	ToUnixNano   int64
	Grain        TimeGrain
	Dimensions   []string
	Measures     []Measure
	Filters      []Filter
	OrderBy      []OrderBy
	Limit        int
	Ungrouped    bool
}

// ColumnKind says what a result column is, so a client can lay it out
// without inspecting values.
type ColumnKind string

const (
	ColumnTime      ColumnKind = "time"
	ColumnDimension ColumnKind = "dimension"
	ColumnMeasure   ColumnKind = "measure"
)

// Column is one result column of a plan.
type Column struct {
	Name string
	Kind ColumnKind
}

// Plan is a compiled request: the SQL to run and the columns it yields.
type Plan struct {
	Dataset string
	// Name is a stable diagnostic identifier for how the query is answered,
	// deliberately not a table name a client could come to depend on.
	Name    string
	SQL     string
	Args    []any
	Columns []Column
}

// Reserved result column names. time_bucket carries the bucket of a grouped
// query, time the event time of an ungrouped row.
const (
	timeColumn = "time"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Compile validates a request against the catalog and turns it into SQL.
// Guardrails are enforced here, not in the transport, so a direct Go caller
// is bound by them too.
func Compile(catalog *Catalog, organizationID, projectID string, req Request) (*Plan, error) {
	ds, ok := catalog.Dataset(req.Dataset)
	if !ok {
		return nil, newError(ErrUnknownDataset, req.Dataset, "dataset", req.Dataset, fmt.Sprintf("dataset %q does not exist", req.Dataset))
	}
	name := ds.Name

	if req.ToUnixNano <= req.FromUnixNano {
		return nil, newError(ErrInvalidTimeRange, name, "to", "", "to must be after from")
	}
	if req.ToUnixNano-req.FromUnixNano > maxTimeRangeNanos {
		return nil, newError(ErrInvalidTimeRange, name, "to", "", fmt.Sprintf("time range exceeds %d days", MaxTimeRangeDays))
	}

	grain := req.Grain
	if grain == "" {
		grain = TimeGrainNone
	}
	if !slices.Contains([]TimeGrain{TimeGrainNone, TimeGrainHour, TimeGrainDay, TimeGrainWeek, TimeGrainMonth}, grain) {
		return nil, newError(ErrUnsupportedGrain, name, "grain", string(grain), fmt.Sprintf("grain %q is not one of none, hour, day, week, month", grain))
	}
	if req.Ungrouped && grain != TimeGrainNone {
		return nil, newError(ErrUnsatisfiable, name, "grain", string(grain), "an ungrouped query has no time buckets")
	}

	if len(req.Dimensions) > MaxDimensions {
		return nil, newError(ErrTooManyDimensions, name, "dimensions", "", fmt.Sprintf("at most %d dimensions", MaxDimensions))
	}
	dimensions := make([]*Field, 0, len(req.Dimensions))
	for i, dim := range req.Dimensions {
		position := fmt.Sprintf("dimensions[%d]", i)
		field, ok := ds.Field(dim)
		if !ok || field.Role != RoleDimension {
			return nil, newError(ErrUnknownField, name, position, dim, fmt.Sprintf("dataset %s has no dimension %s", name, dim))
		}
		if slices.ContainsFunc(dimensions, func(f *Field) bool { return f.Name == dim }) {
			return nil, newError(ErrUnsatisfiable, name, position, dim, fmt.Sprintf("dimension %s is repeated", dim))
		}
		dimensions = append(dimensions, field)
	}

	limit := req.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 0 || limit > MaxLimit {
		return nil, newError(ErrLimitExceeded, name, "limit", fmt.Sprint(req.Limit), fmt.Sprintf("limit must be between 1 and %d", MaxLimit))
	}

	scope := Scope{OrganizationID: organizationID, ProjectID: projectID, FromUnixNano: req.FromUnixNano, ToUnixNano: req.ToUnixNano}
	builder := sq.Select().FromSelect(ds.Source(scope), "src")

	// Filters apply to the collapsed rows, after the source query, so a
	// filter on a dimension whose value settled on the last observation sees
	// that settled value.
	for i, filter := range req.Filters {
		position := fmt.Sprintf("filters[%d]", i)
		field, ok := ds.Field(filter.Field)
		if !ok || field.Role != RoleDimension {
			return nil, newError(ErrUnknownField, name, position+".field", filter.Field, fmt.Sprintf("dataset %s has no dimension %s", name, filter.Field))
		}
		if !field.Admits(filter.Operator) {
			return nil, newError(ErrUnsupportedOperator, name, position+".operator", filter.Operator, fmt.Sprintf("%s does not admit %s", filter.Field, filter.Operator))
		}
		if len(filter.Values) == 0 {
			return nil, newError(ErrUnsatisfiable, name, position+".values", "", "a filter needs at least one value")
		}
		if len(filter.Values) > MaxFilterValues {
			return nil, newError(ErrLimitExceeded, name, position+".values", "", fmt.Sprintf("at most %d values per filter", MaxFilterValues))
		}
		switch Operator(filter.Operator) {
		case OperatorEquals:
			if len(filter.Values) != 1 {
				return nil, newError(ErrUnsatisfiable, name, position+".values", "", "equals takes exactly one value")
			}
			builder = builder.Where(squirrel.Eq{field.Expr: filter.Values[0]})
		case OperatorIn:
			builder = builder.Where(squirrel.Eq{field.Expr: filter.Values})
		}
	}

	plan := &Plan{
		Dataset: name,
		Name:    name + ".agent_events",
		SQL:     "",
		Args:    nil,
		Columns: nil,
	}

	if req.Ungrouped {
		if len(req.Measures) != 0 {
			return nil, newError(ErrUnsatisfiable, name, "measures", "", "an ungrouped query returns rows, not measures")
		}
		if len(req.OrderBy) != 0 {
			return nil, newError(ErrUnsatisfiable, name, "order_by", "", "ungrouped rows are always newest first")
		}
		builder = builder.Column(fmt.Sprintf("fromUnixTimestamp64Nano(%s) AS %s", ds.TimeExpr, timeColumn))
		plan.Columns = append(plan.Columns, Column{Name: timeColumn, Kind: ColumnTime})
		for _, field := range dimensions {
			builder = builder.Column(fmt.Sprintf("%s AS %s", field.Expr, field.Name))
			plan.Columns = append(plan.Columns, Column{Name: field.Name, Kind: ColumnDimension})
		}
		builder = builder.OrderBy(ds.TimeExpr + " DESC").Limit(uint64(limit))
		return finish(plan, builder)
	}

	if len(req.Measures) == 0 {
		return nil, newError(ErrUnsatisfiable, name, "measures", "", "a grouped query needs at least one measure")
	}

	groupBy := make([]string, 0, len(dimensions)+1)
	if grain != TimeGrainNone {
		builder = builder.Column(fmt.Sprintf("%s AS %s", bucketExpr(grain, ds.TimeExpr), timeBucketColumn))
		groupBy = append(groupBy, timeBucketColumn)
		plan.Columns = append(plan.Columns, Column{Name: timeBucketColumn, Kind: ColumnTime})
	}
	for _, field := range dimensions {
		builder = builder.Column(fmt.Sprintf("%s AS %s", field.Expr, field.Name))
		groupBy = append(groupBy, field.Name)
		plan.Columns = append(plan.Columns, Column{Name: field.Name, Kind: ColumnDimension})
	}

	aliases := make([]string, 0, len(req.Measures))
	for i, measure := range req.Measures {
		position := fmt.Sprintf("measures[%d]", i)
		expr, alias, err := measureExpr(ds, measure, position)
		if err != nil {
			return nil, err
		}
		if slices.Contains(aliases, alias) || slices.Contains(groupBy, alias) || alias == timeBucketColumn {
			return nil, newError(ErrUnsatisfiable, name, position+".alias", alias, fmt.Sprintf("result column %s is taken", alias))
		}
		aliases = append(aliases, alias)
		builder = builder.Column(fmt.Sprintf("%s AS %s", expr, alias))
		plan.Columns = append(plan.Columns, Column{Name: alias, Kind: ColumnMeasure})
	}

	if len(groupBy) > 0 {
		builder = builder.GroupBy(groupBy...)
	}

	orderBy := make([]string, 0, len(req.OrderBy)+len(groupBy)+1)
	for i, order := range req.OrderBy {
		position := fmt.Sprintf("order_by[%d]", i)
		if !slices.Contains(aliases, order.Measure) {
			return nil, newError(ErrUnknownField, name, position+".measure", order.Measure, fmt.Sprintf("%s is not a requested measure", order.Measure))
		}
		direction := strings.ToUpper(order.Direction)
		if direction == "" {
			direction = "DESC"
		}
		if direction != "ASC" && direction != "DESC" {
			return nil, newError(ErrUnsatisfiable, name, position+".direction", order.Direction, "direction must be asc or desc")
		}
		orderBy = append(orderBy, order.Measure+" "+direction)
	}
	// A deterministic default: buckets in time order, then the first measure
	// descending, so the most significant groups come first and the result
	// is stable across runs.
	if grain != TimeGrainNone {
		orderBy = append(orderBy, timeBucketColumn+" ASC")
	}
	if len(req.OrderBy) == 0 {
		orderBy = append(orderBy, aliases[0]+" DESC")
	}
	for _, field := range dimensions {
		orderBy = append(orderBy, field.Name+" ASC")
	}
	builder = builder.OrderBy(orderBy...).Limit(uint64(limit))

	return finish(plan, builder)
}

func finish(plan *Plan, builder squirrel.SelectBuilder) (*Plan, error) {
	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build analytics query: %w", err)
	}
	plan.SQL = query
	plan.Args = args
	return plan, nil
}

// measureExpr resolves a requested measure to its SQL and result alias.
func measureExpr(ds *Dataset, measure Measure, position string) (string, string, error) {
	op := strings.ToLower(measure.Op)
	alias := measure.Alias

	if op == AggregationCount {
		if measure.Field != "" {
			return "", "", newError(ErrUnsatisfiable, ds.Name, position+".field", measure.Field, "count takes no field")
		}
		if alias == "" {
			alias = AggregationCount
		}
		if err := checkAlias(ds, alias, position); err != nil {
			return "", "", err
		}
		return "count()", alias, nil
	}

	if measure.Field == "" {
		return "", "", newError(ErrUnsatisfiable, ds.Name, position+".field", "", fmt.Sprintf("%s needs a field", op))
	}
	field, ok := ds.Field(measure.Field)
	if !ok || field.Role != RoleMeasure {
		return "", "", newError(ErrUnknownField, ds.Name, position+".field", measure.Field, fmt.Sprintf("dataset %s has no measure %s", ds.Name, measure.Field))
	}
	if !field.Admits(op) {
		return "", "", newError(ErrUnsupportedAggregation, ds.Name, position+".op", op, fmt.Sprintf("%s does not admit %s", field.Name, op))
	}
	if alias == "" {
		alias = op + "_" + field.Name
	}
	if err := checkAlias(ds, alias, position); err != nil {
		return "", "", err
	}

	var expr string
	switch Aggregation(op) {
	case AggregationSum, AggregationAvg, AggregationMin, AggregationMax:
		expr = fmt.Sprintf("%s(%s)", op, field.Expr)
	case AggregationP50:
		expr = fmt.Sprintf("quantile(0.5)(%s)", field.Expr)
	case AggregationP95:
		expr = fmt.Sprintf("quantile(0.95)(%s)", field.Expr)
	case AggregationP99:
		expr = fmt.Sprintf("quantile(0.99)(%s)", field.Expr)
	default:
		return "", "", newError(ErrUnsupportedAggregation, ds.Name, position+".op", op, fmt.Sprintf("%s is not an aggregation", op))
	}
	return expr, alias, nil
}

// checkAlias keeps a caller-chosen result column name a plain identifier
// that cannot collide with a field, since aliases are interpolated into SQL.
func checkAlias(ds *Dataset, alias, position string) error {
	if !identifierPattern.MatchString(alias) {
		return newError(ErrUnsatisfiable, ds.Name, position+".alias", alias, "alias must be a lowercase identifier")
	}
	if _, taken := ds.Field(alias); taken {
		return newError(ErrUnsatisfiable, ds.Name, position+".alias", alias, fmt.Sprintf("alias %s is a field name", alias))
	}
	if alias == timeColumn || alias == timeBucketColumn {
		return newError(ErrUnsatisfiable, ds.Name, position+".alias", alias, fmt.Sprintf("alias %s is reserved", alias))
	}
	return nil
}

// bucketExpr floors the event time to the grain. Week starts on Monday.
func bucketExpr(grain TimeGrain, timeExpr string) string {
	ts := fmt.Sprintf("fromUnixTimestamp64Nano(%s)", timeExpr)
	switch grain {
	case TimeGrainHour:
		return fmt.Sprintf("toStartOfHour(%s)", ts)
	case TimeGrainDay:
		return fmt.Sprintf("toStartOfDay(%s)", ts)
	case TimeGrainWeek:
		return fmt.Sprintf("toStartOfWeek(%s, 1)", ts)
	case TimeGrainMonth:
		return fmt.Sprintf("toStartOfMonth(%s)", ts)
	case TimeGrainNone:
	}
	return ts
}
