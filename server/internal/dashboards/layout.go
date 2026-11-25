package dashboards

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
	"github.com/speakeasy-api/gram/server/internal/widgets"
)

// The grid every dashboard is laid out on. Widths and positions are in
// columns, heights in rows; the row height is the client's.
const gridColumns = 12

// maxGridRow keeps a layout within what a page can show and the integer
// columns can hold. A dashboard with cards past it is not one anyone made
// by dragging.
const maxGridRow = 10_000

// The instants a query can represent, as nanoseconds since the epoch.
var (
	earliestInstant = time.Unix(0, math.MinInt64).UTC()
	latestInstant   = time.Unix(0, math.MaxInt64).UTC()
)

// maxCards keeps a dashboard, and a layout save that checks every card
// while it holds the dashboard, within reason.
const maxCards = 100

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

// placed is a card that passed its own checks, named by its place in the
// payload for messages.
type placed struct {
	position string
	input    *gen.PlacementInput
}

// checkOverlaps returns which two cards sit on top of each other, or "".
// The grid never lays cards out that way; a caller of the API might.
func checkOverlaps(cards []placed) string {
	for i := 1; i < len(cards); i++ {
		for j := range i {
			a, b := cards[i].input, cards[j].input
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				return fmt.Sprintf("%s overlaps %s", cards[i].position, cards[j].position)
			}
		}
	}
	return ""
}

func chartName(chartType string) string {
	if chartType == "" {
		return "chart"
	}
	return chartType
}

// checkFilters returns why saved filters cannot be stored, or "", and
// settles the spelling of a relative window. The date range is either a
// relative window a widget could be saved with or an absolute from-to; the
// values are per catalog dimension, one some dataset has, within the cap the
// analytics query applies.
func checkFilters(catalog *analytics.Catalog, filters *gen.DashboardFilters) string {
	if filters == nil {
		return ""
	}
	// A range with nothing in it is no range.
	if r := filters.Range; r != nil && r.Preset == nil && r.From == nil && r.To == nil && r.Label == nil {
		filters.Range = nil
	}
	if reason := checkRange(filters.Range); reason != "" {
		return reason
	}
	// A dimension with no values picked, or a null in place of them, is no
	// filter: it is dropped rather than stored as one.
	for field, values := range filters.Values {
		if len(values) == 0 {
			delete(filters.Values, field)
		}
	}
	dimensions := catalogDimensions(catalog)
	if len(filters.Values) > maxFilterDimensions {
		return fmt.Sprintf("filters.values: at most %d dimensions", maxFilterDimensions)
	}
	for field, values := range filters.Values {
		switch {
		case strings.TrimSpace(field) == "":
			return "filters.values: a dimension name is empty"
		case utf8.RuneCountInString(field) > maxFilterNameLength:
			return fmt.Sprintf("filters.values: a dimension name is at most %d characters", maxFilterNameLength)
		case !dimensions[field]:
			return fmt.Sprintf("filters.values.%s: no dataset has a dimension named that", field)
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
		canonical, ok := widgets.CanonicalWindow(*r.Preset)
		if !ok {
			return fmt.Sprintf("filters.range.preset: %q is not a window a dashboard can open on", *r.Preset)
		}
		*r.Preset = canonical
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
		if from.Before(earliestInstant) || to.After(latestInstant) {
			return fmt.Sprintf("filters.range: timestamps must fall between %d and %d", earliestInstant.Year(), latestInstant.Year())
		}
		if r.Label != nil && utf8.RuneCountInString(*r.Label) > maxRangeLabelLength {
			return fmt.Sprintf("filters.range.label: at most %d characters", maxRangeLabelLength)
		}
	case r.Label != nil:
		return "filters.range.label: a label only names an absolute range"
	}
	return ""
}

// catalogDimensions is every dimension name some catalog dataset has: the
// names a dashboard's filter bar can be set to.
func catalogDimensions(catalog *analytics.Catalog) map[string]bool {
	out := map[string]bool{}
	if catalog == nil {
		return out
	}
	for _, dataset := range catalog.Datasets() {
		for _, field := range dataset.Fields {
			if field.Role == analytics.RoleDimension {
				out[field.Name] = true
			}
		}
	}
	return out
}
