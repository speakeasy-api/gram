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
// vocabulary, plus the relative window it is asked over. Decoding refuses
// keys this code does not know, on save and on read alike (see decodeQuery).
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

// windows are the relative windows a widget may ask over: the dashboard's
// date-range presets, so a widget and the page it sits on speak the same
// vocabulary. A widget is a recurring question and keeps its own window
// wherever it is shown, so it follows you forward in time; absolute ranges
// are what the shareable URL is for.
var windows = map[string]time.Duration{
	"15m": 15 * time.Minute,
	"1h":  time.Hour,
	"4h":  4 * time.Hour,
	"1d":  24 * time.Hour,
	"2d":  2 * 24 * time.Hour,
	"3d":  3 * 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"15d": 15 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour,
	// The builder's first spelling of a day, before it took the dashboard's
	// presets. Widgets saved with it still read; new ones save "1d".
	"24h": 24 * time.Hour,
}

// windowNames lists the windows a widget can be saved with, for messages.
const windowNames = "15m, 1h, 4h, 1d, 2d, 3d, 7d, 15d, 30d, 90d"

// CanonicalWindow returns the spelling a relative window is saved with, for
// a dashboard's date range to share a widget's vocabulary: "24h", the
// builder's old spelling of a day, reads as "1d". ok is false for a window
// a widget cannot be saved with.
func CanonicalWindow(window string) (string, bool) {
	if _, ok := windows[window]; !ok {
		return "", false
	}
	if window == "24h" {
		return "1d", true
	}
	return window, true
}

// Validate returns what is wrong with a widget, or "" when it works: the
// question is planned against the catalog, then the chart is checked against
// the question. Used on save, so a mistake is rejected immediately, and on
// read, so a catalog change is visible breakage naming what went missing
// instead of quietly wrong numbers. An error is a failure that is not the
// widget's fault: a write fails with it as a server error, and a read logs
// it and reports only that the widget could not be validated. Exported for
// another service that stores a widget's shape, such as dashboards copying
// cards into saved widgets.
func Validate(catalog *analytics.Catalog, dataset string, rawQuery, rawVisualization []byte, now time.Time) (string, error) {
	query, err := decodeQuery(rawQuery)
	if err != nil {
		return "invalid query: " + err.Error(), nil
	}
	if reason, err := validateQuery(catalog, dataset, query, now); reason != "" || err != nil {
		return reason, err
	}

	var visualization Visualization
	if err := json.Unmarshal(rawVisualization, &visualization); err != nil {
		return "visualization is not a JSON object: " + err.Error(), nil
	}
	return validateVisualization(visualization, query), nil
}

func validateQuery(catalog *analytics.Catalog, dataset string, query Query, now time.Time) (string, error) {
	window, ok := windows[query.Window]
	if !ok {
		return fmt.Sprintf("window %q is not one of %s", query.Window, windowNames), nil
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
			return reason, nil
		}
		req.Measures = append(req.Measures, analytics.Measure{Op: m.Op, Field: m.Field, Alias: m.Alias})
	}
	for i, f := range query.Filters {
		if reason := canonical(fmt.Sprintf("filters[%d].operator", i), f.Operator); reason != "" {
			return reason, nil
		}
		req.Filters = append(req.Filters, analytics.Filter{Field: f.Field, Operator: f.Operator, Values: f.Values})
	}
	for i, o := range query.OrderBy {
		if reason := canonical(fmt.Sprintf("order_by[%d].direction", i), o.Direction); reason != "" {
			return reason, nil
		}
		req.OrderBy = append(req.OrderBy, analytics.OrderBy{Measure: o.Measure, Direction: o.Direction})
	}

	// Tenancy is bound as arguments and never reaches the plan, so any
	// placeholder validates the shape.
	if _, err := analytics.Compile(catalog, "validate", "validate", req); err != nil {
		if invalid, ok := errors.AsType[*analytics.Error](err); ok {
			return invalid.Error(), nil
		}
		return "query could not be validated", fmt.Errorf("plan widget query: %w", err)
	}
	return "", nil
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

// decodeQuery reads a query, refusing a key Query does not know. A
// misspelled key or an absolute from/to would otherwise be dropped, pass
// validation, and be stored and replayed as sent; on read, such a row says
// so in its invalid reason, naming the key.
func decodeQuery(raw []byte) (Query, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var query Query
	if err := dec.Decode(&query); err != nil {
		return Query{}, fmt.Errorf("decode query: %w", err)
	}
	return query, nil
}

// canonical returns why an enum value cannot be saved: it is not in the
// lowercase form the query endpoint accepts.
func canonical(position, value string) string {
	if value != strings.ToLower(value) {
		return fmt.Sprintf("unsatisfiable: %s %q must be lowercase", position, value)
	}
	return ""
}
