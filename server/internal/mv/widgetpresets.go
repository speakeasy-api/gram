package mv

import (
	"encoding/json"

	gen "github.com/speakeasy-api/gram/server/gen/widgets"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// PresetPageSource is a checked-in page layout before conversion to an API view.
type PresetPageSource struct {
	// Page identifies the product page that uses this layout.
	Page string `json:"page"`

	// Rows are ordered from top to bottom.
	Rows []PresetRowSource `json:"rows"`
}

// PresetRowSource holds the widgets in one row of a preset.
type PresetRowSource struct {
	// Widgets are ordered from left to right.
	Widgets []PresetWidgetSource `json:"widgets"`
}

// PresetWidgetSource keeps the stored widget shape and its preset placement.
type PresetWidgetSource struct {
	// Key identifies the card within its page.
	Key string `json:"key"`

	// Name labels the card.
	Name string `json:"name"`

	// Dataset identifies the catalog dataset the query asks.
	Dataset string `json:"dataset"`

	// Span is the width on the preset's 12-column grid.
	Span int `json:"span"`

	// Query is the question in the same JSON shape saved widgets use.
	Query json.RawMessage `json:"query"`

	// Visualization is how to draw the question, as saved widgets store it.
	Visualization json.RawMessage `json:"visualization"`
}

// BuildWidgetPresetView renders a preset page, validating each widget as it is
// read and decoding its objects exactly as a saved widget's objects are decoded.
func BuildWidgetPresetView(page PresetPageSource, check func(dataset string, query, visualization []byte) string) *gen.WidgetPreset {
	out := &gen.WidgetPreset{Page: page.Page, Rows: make([]*gen.PresetRow, 0, len(page.Rows))}
	for _, row := range page.Rows {
		widgets := make([]*gen.PresetWidget, 0, len(row.Widgets))
		for _, widget := range row.Widgets {
			reason := check(widget.Dataset, widget.Query, widget.Visualization)
			query, visualization, reason := decodeWidgetObjects(widget.Query, widget.Visualization, reason)
			widgets = append(widgets, &gen.PresetWidget{
				Key:           widget.Key,
				Name:          widget.Name,
				Dataset:       widget.Dataset,
				Query:         query,
				Visualization: visualization,
				Span:          widget.Span,
				InvalidReason: conv.PtrEmpty(reason),
			})
		}
		out.Rows = append(out.Rows, &gen.PresetRow{Widgets: widgets})
	}
	return out
}
