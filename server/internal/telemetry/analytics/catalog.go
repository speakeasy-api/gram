// Package analytics is the semantic query layer over agent session data. The
// catalog declares datasets and typed fields; callers name those and never a
// table, a column or a line of SQL. The catalog is Go code on purpose: it is
// type-safe, refactors with the compiler, and every change is a reviewed code
// change tied to a surface that needs it.
package analytics

import (
	"fmt"
	"slices"
)

// Kind is the shape of a dataset: occurrences or measurements.
type Kind string

const (
	KindEvent  Kind = "event"
	KindMetric Kind = "metric"
)

// Role is what a field is for: grouping and filtering, or aggregating.
type Role string

const (
	RoleDimension Role = "dimension"
	RoleMeasure   Role = "measure"
)

// FieldType is the value type a field yields.
type FieldType string

const (
	TypeString  FieldType = "string"
	TypeInt64   FieldType = "int64"
	TypeFloat64 FieldType = "float64"
)

// Operator is a filter operator a dimension admits.
type Operator string

const (
	OperatorEquals Operator = "equals"
	OperatorIn     Operator = "in"
)

// Aggregation is an op a measure admits. Count is not listed here: it is an
// op without a field and every dataset admits it at its grain.
type Aggregation string

const (
	AggregationSum Aggregation = "sum"
	AggregationAvg Aggregation = "avg"
	AggregationMin Aggregation = "min"
	AggregationMax Aggregation = "max"
	AggregationP50 Aggregation = "p50"
	AggregationP95 Aggregation = "p95"
	AggregationP99 Aggregation = "p99"
)

// AggregationCount is the one op that takes no field.
const AggregationCount = "count"

// TimeGrain is the width of a time bucket. None means no bucketing.
type TimeGrain string

const (
	TimeGrainNone  TimeGrain = "none"
	TimeGrainHour  TimeGrain = "hour"
	TimeGrainDay   TimeGrain = "day"
	TimeGrainWeek  TimeGrain = "week"
	TimeGrainMonth TimeGrain = "month"
)

// Guardrails are enforced in the compiler, not the transport, so a direct Go
// caller is bound by them too.
const (
	MaxDimensions   = 3
	MaxFilterValues = 100
	DefaultLimit    = 100
	MaxLimit        = 1000

	// How long each kind's table keeps a row, from server/clickhouse/schema.sql:
	// agent_events 90 days, agent_metrics 730 so billing can read historical
	// cycles. A window reaching past a table's retention reads rows the TTL
	// has already dropped and comes back silently incomplete, so a dataset
	// caps its window at what its table still holds. These are the schema's
	// numbers restated, and a test holds them to it.
	MaxEventTimeRangeDays  = 90
	MaxMetricTimeRangeDays = 730

	// MaxTimeRangeDays is the ceiling over every dataset: the longest any
	// window can be. A dataset's own limit is MaxTimeRangeDays(), which is
	// what the compiler enforces.
	MaxTimeRangeDays = MaxMetricTimeRangeDays

	nanosPerDay = int64(24 * 60 * 60 * 1e9)
)

// Field is one queryable thing on a dataset. Expr is the expression over the
// dataset's source query that yields it, which is usually just a column of
// that query's output.
type Field struct {
	Name string
	Type FieldType
	Role Role
	// Default marks a field as part of the query a dataset opens on: a
	// default dimension is in the opening group-by. Declared here rather
	// than guessed by the client, so a new dataset opens sensibly with no
	// client change. A dataset may flag several, or none.
	Default      bool
	Unit         string
	Operators    []Operator
	Aggregations []Aggregation
	Expr         string
}

// Dataset is a logical entity at a declared grain. It owns a source query,
// which is where observations collapse to that grain and the only place a
// de-duplicated sum can be expressed, plus a per-field expression over that
// query's result.
type Dataset struct {
	Name string
	Kind Kind
	// Grain names what one row is, as a noun: "session", "tool call". The
	// builder shows it beside the description, and rows mode is "rows at the
	// dataset's grain".
	Grain       string
	Description string
	// TimeExpr is the column of the source query's output that carries the
	// row's event time, used for bucketing and for ordering row lists.
	TimeExpr string
	Fields   []Field
	// Source builds the dataset's source query for one tenancy and window.
	Source SourceQuery
}

// MaxTimeRangeDays is the longest window this dataset can answer honestly:
// the retention of the table its kind reads.
func (d *Dataset) MaxTimeRangeDays() int {
	if d.Kind == KindMetric {
		return MaxMetricTimeRangeDays
	}
	return MaxEventTimeRangeDays
}

// MaxTimeRangeNanos is MaxTimeRangeDays as a span of nanoseconds, the unit a
// request's window is expressed in.
func (d *Dataset) MaxTimeRangeNanos() int64 {
	return int64(d.MaxTimeRangeDays()) * nanosPerDay
}

// Field finds a field by name.
func (d *Dataset) Field(name string) (*Field, bool) {
	for i := range d.Fields {
		if d.Fields[i].Name == name {
			return &d.Fields[i], true
		}
	}
	return nil, false
}

// Admits says whether a field admits an operator or aggregation.
func (f *Field) Admits(op string) bool {
	switch f.Role {
	case RoleDimension:
		return slices.Contains(f.Operators, Operator(op))
	case RoleMeasure:
		return slices.Contains(f.Aggregations, Aggregation(op))
	}
	return false
}

// Catalog is the declaration of every dataset and field: the contract the
// builder is generated from, and the reason a new dataset needs no client
// change.
type Catalog struct {
	datasets []*Dataset
}

// NewCatalog validates the datasets and returns a catalog over them. A
// declaration that could not be queried is a programming error, so this
// fails loudly rather than serving a half-declared contract.
func NewCatalog(datasets ...*Dataset) (*Catalog, error) {
	seen := make(map[string]struct{}, len(datasets))
	for _, ds := range datasets {
		if ds == nil {
			return nil, fmt.Errorf("catalog: nil dataset")
		}
		if err := ds.validate(); err != nil {
			return nil, err
		}
		if _, dup := seen[ds.Name]; dup {
			return nil, fmt.Errorf("catalog: dataset %q declared twice", ds.Name)
		}
		seen[ds.Name] = struct{}{}
	}
	return &Catalog{datasets: datasets}, nil
}

// MustCatalog is NewCatalog for the package's own declarations.
func MustCatalog(datasets ...*Dataset) *Catalog {
	catalog, err := NewCatalog(datasets...)
	if err != nil {
		panic(err)
	}
	return catalog
}

// Dataset finds a dataset by name.
func (c *Catalog) Dataset(name string) (*Dataset, bool) {
	for _, ds := range c.datasets {
		if ds.Name == name {
			return ds, true
		}
	}
	return nil, false
}

// Datasets lists every dataset in declaration order.
func (c *Catalog) Datasets() []*Dataset {
	return slices.Clone(c.datasets)
}

func (d *Dataset) validate() error {
	if d.Name == "" {
		return fmt.Errorf("catalog: dataset with empty name")
	}
	if d.Kind != KindEvent && d.Kind != KindMetric {
		return fmt.Errorf("catalog: dataset %q has unknown kind %q", d.Name, d.Kind)
	}
	// The builder shows the grain beside the description unconditionally and
	// describe requires it, so a dataset must say what one row is.
	if d.Grain == "" {
		return fmt.Errorf("catalog: dataset %q has no grain", d.Name)
	}
	if d.TimeExpr == "" {
		return fmt.Errorf("catalog: dataset %q has no time expression", d.Name)
	}
	if d.Source == nil {
		return fmt.Errorf("catalog: dataset %q has no source query", d.Name)
	}
	if len(d.Fields) == 0 {
		return fmt.Errorf("catalog: dataset %q declares no fields", d.Name)
	}
	names := make(map[string]struct{}, len(d.Fields))
	for _, f := range d.Fields {
		if f.Name == "" || f.Expr == "" {
			return fmt.Errorf("catalog: dataset %q has a field with an empty name or expression", d.Name)
		}
		if _, dup := names[f.Name]; dup {
			return fmt.Errorf("catalog: dataset %q declares field %q twice", d.Name, f.Name)
		}
		names[f.Name] = struct{}{}
		if f.Name == timeBucketColumn {
			return fmt.Errorf("catalog: dataset %q field %q collides with the time bucket column", d.Name, f.Name)
		}
		// Every capability describe publishes must be one the compiler can
		// honour, so the declared enums are the only values a field may carry.
		switch f.Type {
		case TypeString, TypeInt64, TypeFloat64:
		default:
			return fmt.Errorf("catalog: dataset %q field %q has unknown type %q", d.Name, f.Name, f.Type)
		}
		switch f.Role {
		case RoleDimension:
			if len(f.Operators) == 0 || len(f.Aggregations) != 0 {
				return fmt.Errorf("catalog: dataset %q dimension %q must declare operators and no aggregations", d.Name, f.Name)
			}
			// Filters and value pickers compare a dimension as a string; a
			// numeric one would need casting in both the compiler and the
			// values query, so dimensions are held to strings until one is
			// needed.
			if f.Type != TypeString {
				return fmt.Errorf("catalog: dataset %q dimension %q must be a string", d.Name, f.Name)
			}
			for _, op := range f.Operators {
				switch op {
				case OperatorEquals, OperatorIn:
				default:
					return fmt.Errorf("catalog: dataset %q dimension %q has unknown operator %q", d.Name, f.Name, op)
				}
			}
		case RoleMeasure:
			if len(f.Aggregations) == 0 || len(f.Operators) != 0 {
				return fmt.Errorf("catalog: dataset %q measure %q must declare aggregations and no operators", d.Name, f.Name)
			}
			if f.Type == TypeString {
				return fmt.Errorf("catalog: dataset %q measure %q cannot be a string", d.Name, f.Name)
			}
			for _, agg := range f.Aggregations {
				switch agg {
				case AggregationSum, AggregationAvg, AggregationMin, AggregationMax, AggregationP50, AggregationP95, AggregationP99:
				default:
					return fmt.Errorf("catalog: dataset %q measure %q has unknown aggregation %q", d.Name, f.Name, agg)
				}
			}
		default:
			return fmt.Errorf("catalog: dataset %q field %q has unknown role %q", d.Name, f.Name, f.Role)
		}
	}
	return nil
}

// timeBucketColumn is the reserved result column for a bucketed query.
const timeBucketColumn = "time_bucket"

var equalsIn = []Operator{OperatorEquals, OperatorIn}
