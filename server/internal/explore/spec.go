package explore

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
)

// Spec is the part of a saved query the server understands: the question,
// in catalog vocabulary, plus the relative window it is asked over. Chart
// type and anything else only the client interprets pass through untouched.
// Decoding is deliberately tolerant of keys this code does not know, so an
// old row fails validation with a readable message rather than failing to
// decode at all.
type Spec struct {
	ChartType  string        `json:"chart_type"`
	Window     string        `json:"window"`
	Grain      string        `json:"grain"`
	Dimensions []string      `json:"dimensions"`
	Measures   []SpecMeasure `json:"measures"`
	Filters    []SpecFilter  `json:"filters"`
	OrderBy    []SpecOrderBy `json:"order_by"`
	Limit      int           `json:"limit"`
	Ungrouped  bool          `json:"ungrouped"`
}

type SpecMeasure struct {
	Op    string `json:"op"`
	Field string `json:"field"`
	Alias string `json:"alias"`
}

type SpecFilter struct {
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

type SpecOrderBy struct {
	Measure   string `json:"measure"`
	Direction string `json:"direction"`
}

// windows are the relative windows a saved query may ask over. A query is a
// recurring question and should follow you forward in time; absolute ranges
// are what the shareable URL is for.
var windows = map[string]time.Duration{
	"1h":  time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour,
}

// validateSpec plans the spec against the catalog and returns what is wrong
// with it, or "" when it compiles. Used on save, so a typo is rejected
// immediately, and on read, so a catalog change is visible breakage naming
// the missing field instead of quietly wrong numbers.
func validateSpec(catalog *analytics.Catalog, dataset string, raw []byte, now time.Time) string {
	var spec Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return "spec is not a JSON object: " + err.Error()
	}

	window, ok := windows[spec.Window]
	if !ok {
		return fmt.Sprintf("window %q is not one of 1h, 24h, 7d, 30d, 90d", spec.Window)
	}

	req := analytics.Request{
		Dataset:      dataset,
		FromUnixNano: now.Add(-window).UnixNano(),
		ToUnixNano:   now.UnixNano(),
		Grain:        analytics.TimeGrain(spec.Grain),
		Dimensions:   spec.Dimensions,
		Measures:     make([]analytics.Measure, 0, len(spec.Measures)),
		Filters:      make([]analytics.Filter, 0, len(spec.Filters)),
		OrderBy:      make([]analytics.OrderBy, 0, len(spec.OrderBy)),
		Limit:        spec.Limit,
		Ungrouped:    spec.Ungrouped,
	}
	for _, m := range spec.Measures {
		req.Measures = append(req.Measures, analytics.Measure{Op: m.Op, Field: m.Field, Alias: m.Alias})
	}
	for _, f := range spec.Filters {
		req.Filters = append(req.Filters, analytics.Filter{Field: f.Field, Operator: f.Operator, Values: f.Values})
	}
	for _, o := range spec.OrderBy {
		req.OrderBy = append(req.OrderBy, analytics.OrderBy{Measure: o.Measure, Direction: o.Direction})
	}

	// Tenancy is bound as arguments and never reaches the plan, so any
	// placeholder validates the shape.
	if _, err := analytics.Compile(catalog, "validate", "validate", req); err != nil {
		var invalid *analytics.Error
		if errors.As(err, &invalid) {
			return invalid.Error()
		}
		return err.Error()
	}
	return ""
}
