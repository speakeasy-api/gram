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
	MaxDimensions     = 3
	MaxFilterValues   = 100
	MaxTimeRangeDays  = 730
	DefaultLimit      = 100
	MaxLimit          = 1000
	maxTimeRangeNanos = int64(MaxTimeRangeDays) * 24 * 60 * 60 * 1e9
)

// Field is one queryable thing on a dataset. Expr is the expression over the
// dataset's source query that yields it, which is usually just a column of
// that query's output.
type Field struct {
	Name         string
	Type         FieldType
	Role         Role
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
	// SummaryField names the dimension a row list shows as its headline
	// beside time. Declared here rather than guessed by the client, so a new
	// dataset ships with no client change. Empty when the dataset has no
	// natural headline.
	SummaryField string
	Fields       []Field
	// Source builds the dataset's source query for one tenancy and window.
	Source SourceQuery
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
	if d.TimeExpr == "" {
		return fmt.Errorf("catalog: dataset %q has no time expression", d.Name)
	}
	if d.Source == nil {
		return fmt.Errorf("catalog: dataset %q has no source query", d.Name)
	}
	if len(d.Fields) == 0 {
		return fmt.Errorf("catalog: dataset %q declares no fields", d.Name)
	}
	if d.SummaryField != "" {
		summary, ok := d.Field(d.SummaryField)
		if !ok || summary.Role != RoleDimension {
			return fmt.Errorf("catalog: dataset %q summary field %q is not a declared dimension", d.Name, d.SummaryField)
		}
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
		switch f.Role {
		case RoleDimension:
			if len(f.Operators) == 0 || len(f.Aggregations) != 0 {
				return fmt.Errorf("catalog: dataset %q dimension %q must declare operators and no aggregations", d.Name, f.Name)
			}
		case RoleMeasure:
			if len(f.Aggregations) == 0 || len(f.Operators) != 0 {
				return fmt.Errorf("catalog: dataset %q measure %q must declare aggregations and no operators", d.Name, f.Name)
			}
			if f.Type == TypeString {
				return fmt.Errorf("catalog: dataset %q measure %q cannot be a string", d.Name, f.Name)
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
