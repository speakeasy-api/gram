package widgets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
)

// Query is the part of a widget the server plans: the question, in catalog
// vocabulary, plus the relative window it is asked over. A save refuses keys
// this code does not know (see decodeQueryStrict), but reading a stored row
// stays tolerant of them, so an old row fails validation with a readable
// message rather than failing to decode at all.
type Query struct {
	Window     string         `json:"window"`
	Grain      string         `json:"grain"`
	Dimensions []string       `json:"dimensions"`
	Measures   []QueryMeasure `json:"measures"`
	Filters    []QueryFilter  `json:"filters"`
	OrderBy    []QueryOrderBy `json:"order_by"`
	Limit      int            `json:"limit"`
	Ungrouped  bool           `json:"ungrouped"`
}

type QueryMeasure struct {
	Op    string `json:"op"`
	Field string `json:"field"`
	Alias string `json:"alias"`
}

type QueryFilter struct {
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

type QueryOrderBy struct {
	Measure   string `json:"measure"`
	Direction string `json:"direction"`
}

// ChartType names a chart a widget is drawn with. The constants are the
// types the server reasons about when checking a chart against its question,
// not an allow-list: the client owns the chart vocabulary, so a type with no
// constant here is accepted and left unchecked.
type ChartType string

const (
	ChartLine   ChartType = "line"
	ChartArea   ChartType = "area"
	ChartBar    ChartType = "bar"
	ChartRanked ChartType = "ranked"
	ChartTable  ChartType = "table"
	ChartNumber ChartType = "number"
)

// normalize returns the type lowercased, the form it is checked and stored
// in, so "Line" is checked as a line chart.
func (t ChartType) normalize() ChartType {
	return ChartType(strings.ToLower(string(t)))
}

// Visualization is how a widget's question is drawn. The chart vocabulary
// belongs to the client; the server reads the type only to check that the
// chart can draw the question.
type Visualization struct {
	Type    ChartType       `json:"type"`
	Options json.RawMessage `json:"options"`
}

// windows are the relative windows a widget may ask over. A widget is a
// recurring question and keeps its own window wherever it is shown, so it
// follows you forward in time; absolute ranges are what the shareable URL
// is for.
var windows = map[string]time.Duration{
	"1h":  time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour,
}

// validate returns what is wrong with a widget, or "" when it works: the
// question is planned against the catalog, then the chart is checked against
// the question. Used on save, so a mistake is rejected immediately, and on
// read, so a catalog change is visible breakage naming what went missing
// instead of quietly wrong numbers.
func validate(catalog *analytics.Catalog, dataset string, rawQuery, rawVisualization []byte, now time.Time) string {
	var query Query
	if err := json.Unmarshal(rawQuery, &query); err != nil {
		return "query is not a JSON object: " + err.Error()
	}
	if reason := validateQuery(catalog, dataset, query, now); reason != "" {
		return reason
	}

	var visualization Visualization
	if err := json.Unmarshal(rawVisualization, &visualization); err != nil {
		return "visualization is not a JSON object: " + err.Error()
	}
	return validateVisualization(visualization, query)
}

func validateQuery(catalog *analytics.Catalog, dataset string, query Query, now time.Time) string {
	window, ok := windows[query.Window]
	if !ok {
		return fmt.Sprintf("window %q is not one of 1h, 24h, 7d, 30d, 90d", query.Window)
	}

	req := analytics.Request{
		Dataset:      dataset,
		FromUnixNano: now.Add(-window).UnixNano(),
		ToUnixNano:   now.UnixNano(),
		Grain:        analytics.TimeGrain(query.Grain),
		Dimensions:   query.Dimensions,
		Measures:     make([]analytics.Measure, 0, len(query.Measures)),
		Filters:      make([]analytics.Filter, 0, len(query.Filters)),
		OrderBy:      make([]analytics.OrderBy, 0, len(query.OrderBy)),
		Limit:        query.Limit,
		Ungrouped:    query.Ungrouped,
	}
	// The compiler folds the case of these, but the query endpoint's schema
	// does not: a saved "COUNT" would validate here and be refused on replay.
	// Only the canonical lowercase form is saved.
	for i, m := range query.Measures {
		if reason := canonical(fmt.Sprintf("measures[%d].op", i), m.Op); reason != "" {
			return reason
		}
		req.Measures = append(req.Measures, analytics.Measure{Op: m.Op, Field: m.Field, Alias: m.Alias})
	}
	for i, f := range query.Filters {
		if reason := canonical(fmt.Sprintf("filters[%d].operator", i), f.Operator); reason != "" {
			return reason
		}
		req.Filters = append(req.Filters, analytics.Filter{Field: f.Field, Operator: f.Operator, Values: f.Values})
	}
	for i, o := range query.OrderBy {
		if reason := canonical(fmt.Sprintf("order_by[%d].direction", i), o.Direction); reason != "" {
			return reason
		}
		req.OrderBy = append(req.OrderBy, analytics.OrderBy{Measure: o.Measure, Direction: o.Direction})
	}

	// Tenancy is bound as arguments and never reaches the plan, so any
	// placeholder validates the shape.
	if _, err := analytics.Compile(catalog, "validate", "validate", req); err != nil {
		if invalid, ok := errors.AsType[*analytics.Error](err); ok {
			return invalid.Error()
		}
		return err.Error()
	}
	return ""
}

// validateVisualization returns why a chart cannot draw the question, or "".
// A widget placed somewhere without the builder has none of its guardrails,
// so a chart that cannot draw its question must fail visibly rather than
// render an empty frame. A type this code does not know is not an error: the
// client owns the chart vocabulary.
func validateVisualization(visualization Visualization, query Query) string {
	if visualization.Type == "" {
		return "visualization.type is required"
	}
	if options := strings.TrimSpace(string(visualization.Options)); options != "" && options != "null" && !strings.HasPrefix(options, "{") {
		return "visualization.options must be an object"
	}

	grained := query.Grain != "" && query.Grain != string(analytics.TimeGrainNone)
	aggregated := !query.Ungrouped && len(query.Measures) > 0

	// Compared lowercased, so "Line" gets the same checks as "line" rather
	// than passing as an unknown type and drawing an empty frame. Saving
	// stores the type lowercased.
	chart := visualization.Type.normalize()
	switch chart {
	case ChartLine, ChartArea, ChartBar:
		if !grained || !aggregated {
			return fmt.Sprintf("unsatisfiable: a %s chart draws a timeseries, so its query needs a grain and at least one measure", chart)
		}
	case ChartNumber:
		if !aggregated || grained || len(query.Dimensions) > 0 {
			return "unsatisfiable: a number chart draws whole-window totals, so its query needs at least one measure, no grain and no dimensions"
		}
	case ChartRanked:
		if !aggregated || grained || len(query.Dimensions) == 0 {
			return "unsatisfiable: a ranked chart ranks groups over the whole window, so its query needs at least one measure, no grain and at least one dimension"
		}
	case ChartTable:
		// A table draws any question: rows, totals or a timeseries.
	}
	return ""
}

// decodeQueryStrict refuses a query carrying a key Query does not know. A
// misspelled key or an absolute from/to would otherwise be dropped by the
// tolerant decode, pass validation, and be stored and replayed as sent.
func decodeQueryStrict(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var query Query
	if err := dec.Decode(&query); err != nil {
		return fmt.Errorf("decode query: %w", err)
	}
	return nil
}

// canonical returns why an enum value cannot be saved: it is not in the
// lowercase form the query endpoint accepts.
func canonical(position, value string) string {
	if value != strings.ToLower(value) {
		return fmt.Sprintf("unsatisfiable: %s %q must be lowercase", position, value)
	}
	return ""
}
