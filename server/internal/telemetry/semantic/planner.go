package semantic

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// QueryPlan is a validated query bound to one physical binding, ready to
// compile.
type QueryPlan struct {
	Definition *Definition
	Model      *Model
	Binding    *Binding
	Query      Query
	// Measures are the requested measures in the model's declaration order —
	// the canonical SELECT order.
	Measures []*Measure
	// SortMeasure is the unqualified measure name to ORDER BY, or "".
	SortMeasure string
	// DimensionValuesDims are the catalog dimensions the dimension_values map
	// collects (the binding's dimension set minus GroupBy), sorted. Empty when
	// the query does not request dimension values.
	DimensionValuesDims []string
}

// UnsatisfiableError reports that no binding of the model can serve the
// query, naming exactly which requested parts are unsatisfiable.
type UnsatisfiableError struct {
	Model string
	// MissingDimensions and MissingMeasures are requested names no binding of
	// the model serves.
	MissingDimensions []string
	MissingMeasures   []string
	// GranularitySeconds is set when the requested granularity is finer than
	// every binding that serves the requested dimensions and measures.
	GranularitySeconds int64
	// MinWindowSeconds is set when the requested time window is narrower
	// than the min_window_seconds of every otherwise-fitting binding; it
	// carries the smallest such requirement.
	MinWindowSeconds int64
}

func (e *UnsatisfiableError) Error() string {
	parts := make([]string, 0, 3)
	if len(e.MissingDimensions) > 0 {
		parts = append(parts, fmt.Sprintf("dimensions %s not served by any binding", strings.Join(e.MissingDimensions, ", ")))
	}
	if len(e.MissingMeasures) > 0 {
		parts = append(parts, fmt.Sprintf("measures %s not served by any binding", strings.Join(e.MissingMeasures, ", ")))
	}
	if e.GranularitySeconds > 0 {
		parts = append(parts, fmt.Sprintf("granularity %ds is finer than any binding serving the request supports", e.GranularitySeconds))
	}
	if e.MinWindowSeconds > 0 {
		parts = append(parts, fmt.Sprintf("time window is narrower than the %ds minimum an otherwise-fitting binding requires", e.MinWindowSeconds))
	}
	if len(parts) == 0 {
		parts = append(parts, "no binding satisfies the query")
	}
	return fmt.Sprintf("model %s cannot satisfy the query: %s", e.Model, strings.Join(parts, "; "))
}

// Plan validates the query against the definition and selects the highest-
// precedence binding that serves every requested dimension, measure, and the
// requested granularity. One query reads exactly one model (q.Model), so
// mutually exclusive usage authorities can never be combined: the
// exclusive_with declarations are definition-level documentation enforced by
// the query shape itself.
func Plan(def *Definition, q Query) (*QueryPlan, error) {
	if q.Model == "" {
		return nil, fmt.Errorf("query names no model")
	}
	if len(q.Measures) == 0 {
		return nil, fmt.Errorf("query requests no measures")
	}

	model, ok := def.Model(q.Model)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", q.Model)
	}
	modelName := model.Name

	// Resolve requested measures, deduplicated, in model declaration order.
	requested := make(map[string]bool, len(q.Measures))
	for _, name := range q.Measures {
		if _, ok := model.Measure(name); !ok {
			return nil, fmt.Errorf("unknown measure %q for model %q", name, modelName)
		}
		requested[name] = true
	}
	measures := make([]*Measure, 0, len(requested))
	orderedNames := make([]string, 0, len(requested))
	for i := range model.Measures {
		if requested[model.Measures[i].Name] {
			measures = append(measures, &model.Measures[i])
			orderedNames = append(orderedNames, model.Measures[i].Name)
		}
	}

	// Validate the requested dimensions against the model's allowlist (which
	// load-time validation guarantees is a subset of the catalog).
	requiredDims := make([]string, 0, len(q.Filters)+2)
	if q.GroupBy != "" {
		if !slices.Contains(model.Dimensions, q.GroupBy) {
			return nil, fmt.Errorf("unknown group_by dimension %q for model %q", q.GroupBy, modelName)
		}
		requiredDims = append(requiredDims, q.GroupBy)
	}
	for _, f := range q.Filters {
		if !slices.Contains(model.Dimensions, f.Dimension) {
			return nil, fmt.Errorf("unknown filter dimension %q for model %q", f.Dimension, modelName)
		}
		if !slices.Contains(requiredDims, f.Dimension) {
			requiredDims = append(requiredDims, f.Dimension)
		}
	}
	// The compiler always scopes on the project dimension, so every candidate
	// binding must serve it.
	if !slices.Contains(requiredDims, "project") {
		requiredDims = append(requiredDims, "project")
	}

	sortMeasure := ""
	if q.Sort != nil {
		if !slices.Contains(orderedNames, q.Sort.Measure) {
			return nil, fmt.Errorf("sort measure %q is not among the requested measures", q.Sort.Measure)
		}
		sortMeasure = q.Sort.Measure
	}

	binding, err := selectBinding(model, requiredDims, orderedNames, q.GranularitySeconds, q.TimeEnd-q.TimeStart)
	if err != nil {
		return nil, err
	}

	// dimension_values enumerates the binding's dimension set minus GroupBy —
	// deliberately what the binding HAS, not a satisfiability requirement.
	var dimensionValuesDims []string
	if q.IncludeDimensionValues {
		dimensionValuesDims = make([]string, 0, len(binding.Dimensions))
		for name := range binding.Dimensions {
			if name == q.GroupBy {
				continue
			}
			dimensionValuesDims = append(dimensionValuesDims, name)
		}
		sort.Strings(dimensionValuesDims)
	}

	return &QueryPlan{
		Definition:          def,
		Model:               model,
		Binding:             binding,
		Query:               q,
		Measures:            measures,
		SortMeasure:         sortMeasure,
		DimensionValuesDims: dimensionValuesDims,
	}, nil
}

// selectBinding picks the highest-precedence binding serving every required
// dimension, every requested measure, the requested granularity, and — for
// bindings that declare min_window_seconds — a wide-enough time window.
func selectBinding(model *Model, requiredDims, measureNames []string, granularitySeconds, windowNanos int64) (*Binding, error) {
	ordered := make([]*Binding, 0, len(model.Bindings))
	for i := range model.Bindings {
		ordered = append(ordered, &model.Bindings[i])
	}
	slices.SortStableFunc(ordered, func(a, b *Binding) int {
		return b.Precedence - a.Precedence
	})

	granularityBlocked := false
	var windowBlockedBy int64
	for _, b := range ordered {
		servesAll := true
		for _, dim := range requiredDims {
			if _, ok := b.Dimensions[dim]; !ok {
				servesAll = false
				break
			}
		}
		if servesAll {
			for _, ms := range measureNames {
				if _, ok := b.Measures[ms]; !ok {
					servesAll = false
					break
				}
			}
		}
		if !servesAll {
			continue
		}
		if granularitySeconds != 0 && granularitySeconds < b.Time.MinGranularitySeconds {
			granularityBlocked = true
			continue
		}
		if b.MinWindowSeconds > 0 && windowNanos < b.MinWindowSeconds*int64(time.Second) {
			if windowBlockedBy == 0 || b.MinWindowSeconds < windowBlockedBy {
				windowBlockedBy = b.MinWindowSeconds
			}
			continue
		}
		return b, nil
	}

	// Name the unsatisfiable parts exactly: dimensions/measures no binding
	// serves, and granularity/window when either alone excluded
	// otherwise-fitting bindings.
	unsat := &UnsatisfiableError{
		Model:              model.Name,
		MissingDimensions:  nil,
		MissingMeasures:    nil,
		GranularitySeconds: 0,
		MinWindowSeconds:   0,
	}
	for _, dim := range requiredDims {
		missing := true
		for _, b := range ordered {
			if _, ok := b.Dimensions[dim]; ok {
				missing = false
				break
			}
		}
		if missing {
			unsat.MissingDimensions = append(unsat.MissingDimensions, dim)
		}
	}
	for _, ms := range measureNames {
		missing := true
		for _, b := range ordered {
			if _, ok := b.Measures[ms]; ok {
				missing = false
				break
			}
		}
		if missing {
			unsat.MissingMeasures = append(unsat.MissingMeasures, ms)
		}
	}
	if granularityBlocked {
		unsat.GranularitySeconds = granularitySeconds
	}
	unsat.MinWindowSeconds = windowBlockedBy
	return nil, unsat
}
