package widgets

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
)

// Built-in product pages are presets: layouts of widgets checked into the
// repository rather than rows in the widgets table, the way Braintrust ships
// a preset "Cost and quality" dashboard. A preset widget has the shape the
// widgets service stores, so a dashboard can later reuse the layout and a
// preset widget can become a saved one without translation.
//
// Every preset is validated against the catalog by TestPresets, the same
// check a saved widget gets on write and read, so a catalog change that
// breaks a page fails the build rather than production.

//go:embed presets.json
var presetsJSON []byte

// presetSpans are the widths a widget may take on the 12-column grid: a
// quarter, a third, a half and the full width.
var presetSpans = []int{3, 4, 6, 12}

// gridColumns is the width of a preset row.
const gridColumns = 12

type presetFile struct {
	Pages []mv.PresetPageSource `json:"pages"`
}

// parsePresets reads a presets file strictly: an unknown key, a duplicate
// page or card, a span off the grid or a row wider than it is an error,
// not something to guess around.
func parsePresets(raw []byte) (map[string]mv.PresetPageSource, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var file presetFile
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("decode presets: %w", err)
	}

	pages := make(map[string]mv.PresetPageSource, len(file.Pages))
	for _, page := range file.Pages {
		if page.Page == "" {
			return nil, errors.New("a preset page has no name")
		}
		if _, ok := pages[page.Page]; ok {
			return nil, fmt.Errorf("preset page %q is declared twice", page.Page)
		}
		if len(page.Rows) == 0 {
			return nil, fmt.Errorf("preset page %q has no rows", page.Page)
		}
		keys := map[string]bool{}
		for r, row := range page.Rows {
			if len(row.Widgets) == 0 {
				return nil, fmt.Errorf("preset page %q row %d has no widgets", page.Page, r)
			}
			width := 0
			for _, widget := range row.Widgets {
				where := fmt.Sprintf("preset page %q widget %q", page.Page, widget.Key)
				switch {
				case widget.Key == "":
					return nil, fmt.Errorf("preset page %q row %d has a widget with no key", page.Page, r)
				case keys[widget.Key]:
					return nil, fmt.Errorf("%s is declared twice", where)
				case widget.Name == "":
					return nil, fmt.Errorf("%s has no name", where)
				case widget.Dataset == "":
					return nil, fmt.Errorf("%s has no dataset", where)
				case !slices.Contains(presetSpans, widget.Span):
					return nil, fmt.Errorf("%s spans %d columns, not one of %v", where, widget.Span, presetSpans)
				case len(widget.Query) == 0 || len(widget.Visualization) == 0:
					return nil, fmt.Errorf("%s needs a query and a visualization", where)
				}
				keys[widget.Key] = true
				width += widget.Span
			}
			if width > gridColumns {
				return nil, fmt.Errorf("preset page %q row %d spans %d columns, more than the grid's %d", page.Page, r, width, gridColumns)
			}
		}
		pages[page.Page] = page
	}
	return pages, nil
}

// presetProblems returns what is wrong with each preset widget against the
// catalog, one line per broken widget; none means every page works.
func presetProblems(catalog *analytics.Catalog, pages map[string]mv.PresetPageSource, now time.Time) ([]string, error) {
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(pages)) {
		for _, row := range pages[name].Rows {
			for _, widget := range row.Widgets {
				reason, err := validate(catalog, widget.Dataset, widget.Query, widget.Visualization, now)
				if err != nil {
					return nil, fmt.Errorf("validate preset page %q widget %q: %w", name, widget.Key, err)
				}
				if reason != "" {
					problems = append(problems, fmt.Sprintf("preset page %q widget %q: %s", name, widget.Key, reason))
				}
			}
		}
	}
	return problems, nil
}
