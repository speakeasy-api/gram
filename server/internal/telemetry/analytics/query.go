// Package analytics defines the bounded semantic query language used by the
// telemetry architecture spike.
package analytics

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrInvalidScope marks a missing or malformed tenant scope.
	ErrInvalidScope = errors.New("invalid telemetry query scope")

	// ErrInvalidTimeRange marks an empty or reversed query interval.
	ErrInvalidTimeRange = errors.New("invalid telemetry query time range")

	// ErrInvalidQuery marks a query that is not present in the semantic catalog.
	ErrInvalidQuery = errors.New("invalid telemetry query")

	// ErrNoCompatiblePlan marks a valid semantic query with no available source.
	ErrNoCompatiblePlan = errors.New("no compatible telemetry query plan")
)

// DatasetID identifies a product dataset without exposing a physical table.
type DatasetID string

// DatasetUsage is the canonical model-usage dataset.
const DatasetUsage DatasetID = "usage"

// DimensionID identifies a cataloged grouping and filtering dimension.
type DimensionID string

const (
	// DimensionProvider groups usage by the normalized model provider.
	DimensionProvider DimensionID = "provider"

	// DimensionModel groups usage by the normalized model identifier.
	DimensionModel DimensionID = "model"

	// DimensionSource groups usage by the normalized observation source.
	DimensionSource DimensionID = "source"
)

// MeasureID identifies a cataloged aggregation.
type MeasureID string

const (
	// MeasureRequests counts canonical usage observations.
	MeasureRequests MeasureID = "requests"

	// MeasureInputTokens sums non-cache input tokens.
	MeasureInputTokens MeasureID = "input_tokens"

	// MeasureOutputTokens sums output tokens.
	MeasureOutputTokens MeasureID = "output_tokens"

	// MeasureCacheReadInputTokens sums cache-read input tokens.
	MeasureCacheReadInputTokens MeasureID = "cache_read_input_tokens"

	// MeasureCacheCreationInputTokens sums cache-creation input tokens.
	MeasureCacheCreationInputTokens MeasureID = "cache_creation_input_tokens"

	// MeasureTotalTokens sums every token category.
	MeasureTotalTokens MeasureID = "total_tokens"

	// MeasureTotalCost sums fixed-precision model cost.
	MeasureTotalCost MeasureID = "total_cost"
)

// Grain identifies a supported time bucket.
type Grain string

const (
	// GrainNone aggregates the complete requested interval.
	GrainNone Grain = ""

	// GrainDay groups results into UTC calendar days.
	GrainDay Grain = "day"
)

// Operator identifies a cataloged filter operation.
type Operator string

const (
	// OperatorEquals compares a dimension with one value.
	OperatorEquals Operator = "equals"

	// OperatorIn compares a dimension with a bounded set of values.
	OperatorIn Operator = "in"
)

// Direction identifies an allowed result ordering direction.
type Direction string

const (
	// DirectionAscending sorts the requested measure from low to high.
	DirectionAscending Direction = "asc"

	// DirectionDescending sorts the requested measure from high to low.
	DirectionDescending Direction = "desc"
)

// Scope is an authenticated organization and its authorized project set.
// Its fields are private so a valid scope can only be created by NewScope.
type Scope struct {
	organizationID uuid.UUID
	projectIDs     []uuid.UUID
}

// NewScope creates an immutable tenant scope and removes duplicate projects.
func NewScope(organizationID uuid.UUID, projectIDs ...uuid.UUID) (Scope, error) {
	if organizationID == uuid.Nil {
		return Scope{}, fmt.Errorf("%w: organization id is required", ErrInvalidScope)
	}
	if len(projectIDs) == 0 {
		return Scope{}, fmt.Errorf("%w: at least one project id is required", ErrInvalidScope)
	}

	seen := make(map[uuid.UUID]struct{}, len(projectIDs))
	projects := make([]uuid.UUID, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		if projectID == uuid.Nil {
			return Scope{}, fmt.Errorf("%w: project id is required", ErrInvalidScope)
		}
		if _, ok := seen[projectID]; ok {
			continue
		}
		seen[projectID] = struct{}{}
		projects = append(projects, projectID)
	}

	return Scope{
		organizationID: organizationID,
		projectIDs:     projects,
	}, nil
}

// TimeRange is a half-open UTC interval [from, to).
// Its fields are private so a valid range can only be created by NewTimeRange.
type TimeRange struct {
	from time.Time
	to   time.Time
}

// NewTimeRange creates a half-open UTC interval.
func NewTimeRange(from, to time.Time) (TimeRange, error) {
	if from.IsZero() || to.IsZero() {
		return TimeRange{}, fmt.Errorf("%w: both bounds are required", ErrInvalidTimeRange)
	}
	from = from.UTC()
	to = to.UTC()
	if !from.Before(to) {
		return TimeRange{}, fmt.Errorf("%w: from must be before to", ErrInvalidTimeRange)
	}

	return TimeRange{from: from, to: to}, nil
}

// Filter is a cataloged dimension predicate with parameterized values.
type Filter struct {
	dimension DimensionID
	operator  Operator
	values    []string
}

// Equals creates an equality predicate for a cataloged dimension.
func Equals(dimension DimensionID, value string) Filter {
	return Filter{
		dimension: dimension,
		operator:  OperatorEquals,
		values:    []string{value},
	}
}

// OneOf creates a set-membership predicate for a cataloged dimension.
func OneOf(dimension DimensionID, values ...string) Filter {
	return Filter{
		dimension: dimension,
		operator:  OperatorIn,
		values:    append([]string(nil), values...),
	}
}

// Order requests sorting by a selected measure.
type Order struct {
	measure   MeasureID
	direction Direction
}

// Ascending sorts a selected measure from low to high.
func Ascending(measure MeasureID) Order {
	return Order{measure: measure, direction: DirectionAscending}
}

// Descending sorts a selected measure from high to low.
func Descending(measure MeasureID) Order {
	return Order{measure: measure, direction: DirectionDescending}
}

// Query is an immutable semantic request. Physical tables and SQL expressions
// are deliberately absent from this type.
type Query struct {
	dataset    DatasetID
	scope      Scope
	timeRange  TimeRange
	dimensions []DimensionID
	measures   []MeasureID
	filters    []Filter
	grain      Grain
	orders     []Order
	limit      int
}

// NewUsageQuery creates a usage query with mandatory tenant and time bounds.
func NewUsageQuery(scope Scope, timeRange TimeRange) Query {
	return Query{
		dataset:    DatasetUsage,
		scope:      scope,
		timeRange:  timeRange,
		dimensions: nil,
		measures:   nil,
		filters:    nil,
		grain:      GrainNone,
		orders:     nil,
		limit:      0,
	}
}

// GroupBy returns a query grouped by the supplied cataloged dimensions.
func (q Query) GroupBy(dimensions ...DimensionID) Query {
	q.dimensions = append(append([]DimensionID(nil), q.dimensions...), dimensions...)
	return q
}

// Select returns a query containing the supplied cataloged measures.
func (q Query) Select(measures ...MeasureID) Query {
	q.measures = append(append([]MeasureID(nil), q.measures...), measures...)
	return q
}

// Where returns a query constrained by the supplied cataloged filters.
func (q Query) Where(filters ...Filter) Query {
	q.filters = append(append([]Filter(nil), q.filters...), filters...)
	return q
}

// AtGrain returns a query grouped by the supplied time grain.
func (q Query) AtGrain(grain Grain) Query {
	q.grain = grain
	return q
}

// OrderBy returns a query ordered by selected measures.
func (q Query) OrderBy(orders ...Order) Query {
	q.orders = append(append([]Order(nil), q.orders...), orders...)
	return q
}

// WithLimit returns a query with a bounded result-row limit.
func (q Query) WithLimit(limit int) Query {
	q.limit = limit
	return q
}
