package dashboards

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	"github.com/speakeasy-api/gram/server/internal/widgets"
)

// The grid every dashboard is laid out on. Widths and positions are in
// columns, heights in rows; the row height is the client's.
const gridColumns = 12

// maxGridRow keeps a layout within what a page can show and the integer
// columns can hold. A dashboard with cards past it is not one anyone made
// by dragging.
const maxGridRow = 10_000

// The saved filters are replayed to every reader of the dashboard, so each
// part of them is bounded: how many dimensions, how long a name, a value and
// a range label may be, and (maxFilterValues) how many values a dimension
// may pick.
const (
	maxFilterDimensions  = 20
	maxFilterNameLength  = 100
	maxFilterValueLength = 500
	maxRangeLabelLength  = 200
)

// Filter values per dimension are capped where the analytics query caps
// them, so a saved filter is one every card can run.
const maxFilterValues = 100

// size is a card's width in columns and height in rows.
type size struct{ w, h int }

// cardSizes are the minimum and the opening size of a card, by the chart
// type its widget draws. A number tile is one figure, so it stays small;
// anything with an axis, a list or a table needs room to read.
func cardSizes(chartType string) (minimum, opening size) {
	if chartType == "number" {
		return size{w: 2, h: 2}, size{w: 3, h: 2}
	}
	return size{w: 4, h: 3}, size{w: 6, h: 3}
}

// chartTypeOf reads the chart type a stored visualization names, lowercased
// as the widgets service stores it, or "" when it cannot be read.
func chartTypeOf(visualization []byte) string {
	var stored struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(visualization, &stored); err != nil {
		return ""
	}
	return strings.ToLower(stored.Type)
}

// checkPlacement returns why a card cannot sit where a layout puts it, or
// "". The grid is 12 columns wide, and each chart type has a minimum size
// so nothing becomes unreadable.
func checkPlacement(position string, input *gen.PlacementInput, chartType string) string {
	minimum, _ := cardSizes(chartType)
	switch {
	case input.X < 0 || input.Y < 0:
		return fmt.Sprintf("%s: position must not be negative", position)
	case input.W < minimum.w || input.H < minimum.h:
		return fmt.Sprintf("%s: a %s card is at least %d columns by %d rows", position, chartName(chartType), minimum.w, minimum.h)
	// Compared without adding, so absurd sizes cannot wrap around the check.
	case input.W > gridColumns || input.X > gridColumns-input.W:
		return fmt.Sprintf("%s: the card runs past the grid's %d columns", position, gridColumns)
	case input.H > maxGridRow || input.Y > maxGridRow-input.H:
		return fmt.Sprintf("%s: the card sits past row %d", position, maxGridRow)
	}
	return ""
}

func chartName(chartType string) string {
	if chartType == "" {
		return "chart"
	}
	return chartType
}

// checkFilters returns why saved filters cannot be stored, or "". The date
// range is either a relative window a widget could be saved with or an
// absolute from-to; the values are per catalog dimension, within the cap
// the analytics query applies.
func checkFilters(filters *gen.DashboardFilters) string {
	if filters == nil {
		return ""
	}
	if reason := checkRange(filters.Range); reason != "" {
		return reason
	}
	if len(filters.Values) > maxFilterDimensions {
		return fmt.Sprintf("filters.values: at most %d dimensions", maxFilterDimensions)
	}
	for field, values := range filters.Values {
		switch {
		case strings.TrimSpace(field) == "":
			return "filters.values: a dimension name is empty"
		case utf8.RuneCountInString(field) > maxFilterNameLength:
			return fmt.Sprintf("filters.values: a dimension name is at most %d characters", maxFilterNameLength)
		case len(values) > maxFilterValues:
			return fmt.Sprintf("filters.values.%s: at most %d values", field, maxFilterValues)
		}
		for _, value := range values {
			switch {
			case strings.TrimSpace(value) == "":
				return fmt.Sprintf("filters.values.%s: a value is empty", field)
			case utf8.RuneCountInString(value) > maxFilterValueLength:
				return fmt.Sprintf("filters.values.%s: a value is at most %d characters", field, maxFilterValueLength)
			}
		}
	}
	return ""
}

func checkRange(r *gen.DashboardRange) string {
	if r == nil {
		return ""
	}
	absolute := r.From != nil || r.To != nil
	switch {
	case r.Preset != nil && absolute:
		return "filters.range: a preset and an absolute range cannot both be set"
	case r.Preset != nil:
		if !widgets.IsWindow(*r.Preset) {
			return fmt.Sprintf("filters.range.preset: %q is not a window a dashboard can open on", *r.Preset)
		}
		if r.Label != nil {
			return "filters.range.label: a label only names an absolute range"
		}
	case absolute:
		if r.From == nil || r.To == nil {
			return "filters.range: an absolute range needs both from and to"
		}
		from, err := time.Parse(time.RFC3339, *r.From)
		if err != nil {
			return "filters.range.from: not an RFC 3339 timestamp"
		}
		to, err := time.Parse(time.RFC3339, *r.To)
		if err != nil {
			return "filters.range.to: not an RFC 3339 timestamp"
		}
		if !from.Before(to) {
			return "filters.range: from must be before to"
		}
		if r.Label != nil && utf8.RuneCountInString(*r.Label) > maxRangeLabelLength {
			return fmt.Sprintf("filters.range.label: at most %d characters", maxRangeLabelLength)
		}
	case r.Label != nil:
		return "filters.range.label: a label only names an absolute range"
	}
	return ""
}
