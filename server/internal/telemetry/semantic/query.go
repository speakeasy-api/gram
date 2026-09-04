package semantic

// Query is the internal semantic query shape — the future JSON API contract.
// All names are catalog vocabulary; legacy telemetry.query keys never appear
// here (the adapter in the telemetry package translates them).
type Query struct {
	// Model is the fact model the query addresses (e.g. "usage"). One query
	// reads one model; combining models is syntactically impossible, which is
	// how mutually exclusive usage authorities stay separate.
	Model string
	// Measures are bare measure names of the model ("cost_usd").
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

// Sort ranks table rows by one of the requested measures (bare name).
type Sort struct {
	Measure string
	Desc    bool
}
