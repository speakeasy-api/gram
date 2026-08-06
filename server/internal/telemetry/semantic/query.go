package semantic

// Query is the internal semantic query shape — the future JSON API contract.
// All names are catalog vocabulary; legacy telemetry.query keys never appear
// here (the adapter in the telemetry package translates them).
type Query struct {
	// Measures are model-qualified measure names ("turn.usage.cost_usd").
	// All measures must belong to the same model for now.
	Measures []string
	// GroupBy is one catalog dimension, or "" for a single aggregate group.
	GroupBy string
	// Filters are ANDed predicates on catalog dimensions.
	Filters []Filter
	// TimeStart and TimeEnd bound the query window in unix nanoseconds,
	// inclusive on both ends.
	TimeStart int64
	TimeEnd   int64
	// GranularitySeconds selects the query shape: 0 aggregates over the whole
	// range (table query); >0 buckets into a timeseries.
	GranularitySeconds int64
	// Scope is injected by the service layer, never client input.
	Scope Scope
	// Sort ranks table rows by a measure; table queries only.
	Sort *Sort
	// IncludeDimensionValues collects, per group, the distinct values of every
	// other dimension the chosen binding serves (table queries only).
	IncludeDimensionValues bool
}

// Filter matches rows whose dimension equals any of Values (IN semantics; for
// array dimensions, matches if any element is present).
type Filter struct {
	Dimension string
	Values    []string
}

// Scope is the authorization scope the service layer resolved for the caller.
type Scope struct {
	ProjectIDs []string
}

// Sort ranks table rows by a model-qualified measure.
type Sort struct {
	Measure string
	Desc    bool
}
