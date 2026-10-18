// Package signals compiles explicit signal measurements over canonical winners.
package signals

import "time"

const (
	// Dataset names the semantic signal-measurement contract.
	Dataset = "signals"
	// MaxGroups bounds chart cardinality and exact distinct-state fanout.
	MaxGroups = 20
	// DefaultGroups keeps ordinary breakdowns readable.
	DefaultGroups = 5
	// MaxPoints bounds the complete filled result, across all selected series.
	MaxPoints = 10000
	// DefaultPoints fits ordinary charts while allowing six-hour minute views.
	DefaultPoints = 2000
	// MaxSpanDays bounds one exploratory request independently of request-time TTL.
	MaxSpanDays = 3660
	// MaxFilters bounds predicate complexity for arbitrary attribute selectors.
	MaxFilters = 20
	// MaxValues bounds one inclusion filter.
	MaxValues = 100
)

// Dimension identifies a typed system dimension or an arbitrary string label.
type Dimension struct {
	// Field is a system dimension; mutually exclusive with Attribute.
	Field string `json:"field,omitempty"`

	// Attribute is the exact ingestion-label key, including punctuation.
	Attribute string `json:"attribute,omitempty"`
}

// Filter constrains winning readings; filters combine with AND.
type Filter struct {
	// Dimension selects the field or label.
	Dimension Dimension `json:"dimension"`

	// Operator is equals, in, exists or missing.
	Operator string `json:"operator"`

	// Values contains exact case-sensitive values for equals/in.
	Values []string `json:"values,omitempty"`
}

// Selection names one sensor and the exact rule that constitutes a match.
type Selection struct {
	// SensorID is required, even when two sensors share a signal.
	SensorID string `json:"sensor_id"`

	// SignalID selects a multi-label or exclusive result.
	SignalID string `json:"signal_id,omitempty"`

	// MinimumLevel selects an ordered distribution's zero-based cumulative cutoff.
	MinimumLevel *int `json:"minimum_level,omitempty"`

	// Threshold applies to multi-label or cumulative ordered probability.
	Threshold float64 `json:"threshold"`
}

// Request is a measurement, not a freely composed aggregate.
type Request struct {
	// Selection pins the measurement and its counting population.
	Selection Selection `json:"selection"`

	// Trigger is stream (default) or manual.
	Trigger string `json:"trigger,omitempty"`

	// RunID is required only for manual evaluation.
	RunID string `json:"run_id,omitempty"`

	// GroupBy selects complete attribute combinations, not independent marginals.
	GroupBy []Dimension `json:"group_by,omitempty"`

	// Filters are applied after latest-attempt replacement.
	Filters []Filter `json:"filters,omitempty"`

	// Groups bounds selected groups, ranked over the complete requested window.
	Groups int `json:"groups,omitempty"`

	// Points bounds the total number of complete chart points.
	Points int `json:"points,omitempty"`
}

// Context binds authorization and clocks outside the caller's label namespace.
type Context struct {
	// OrganizationID comes from the authenticated caller.
	OrganizationID string

	// ProjectID is the authorized project.
	ProjectID string

	// From and To delimit a half-open source-time window.
	From time.Time

	// To is exclusive.
	To time.Time

	// Now is query time, used for logical retention and freshness metadata.
	Now time.Time

	// Step is auto, minute, hour, day, week or calendar month.
	Step string

	// RequestedAt is a proven manual-run request timestamp, never inferred from source time.
	RequestedAt *time.Time
}

// Query is a parameterized physical read, executed by the shared analytics runner.
type Query struct {
	// SQL contains reviewed expressions with bound customer input.
	SQL string

	// Args binds tenancy, time, identities, label keys and values.
	Args []any
}

// Plan separates physical source choice from output resolution.
type Plan struct {
	// Context is the normalized scope and effective output interval.
	Context Context

	// Request is the validated measurement with effective limits.
	Request Request

	// Groups returns the whole-window summary and the top groups plus a sentinel.
	Groups Query

	// Prepared is the filtered, winner-resolved source shared by subsequent reads.
	Prepared Query
}

// Error describes an actionable validation or cost refusal.
type Error struct {
	// Field identifies the input to correct.
	Field string

	// Message explains the refusal without exposing SQL or database details.
	Message string
}

func (e *Error) Error() string { return e.Field + ": " + e.Message }
