package widgets

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
)

// TestPresets is the gate on the built-in pages: every preset parses, and
// every widget in it validates against the catalog exactly as a saved
// widget does on write and read. A catalog change that breaks a page fails
// here, not in production.
func TestPresets(t *testing.T) {
	t.Parallel()

	pages, err := parsePresets(presetsJSON)
	require.NoError(t, err)
	problems, err := presetProblems(analytics.Default, pages, time.Now())
	require.NoError(t, err)
	require.Empty(t, problems, "every preset widget must validate against the catalog")
}

const presetCount = `{"window":"24h","grain":"none","measures":[{"op":"count","alias":"count"}]}`
const presetNumber = `{"type":"number","options":{}}`

// presetFixture is a presets file with one page, "home", of one row.
func presetFixture(widgets ...string) []byte {
	return []byte(`{"pages":[{"page":"home","rows":[{"widgets":[` + strings.Join(widgets, ",") + `]}]}]}`)
}

func presetWidgetJSON(key string, span int, query, visualization string) string {
	return `{"key":"` + key + `","name":"` + key + `","dataset":"sessions","span":` + strconv.Itoa(span) + `,"query":` + query + `,"visualization":` + visualization + `}`
}

func TestParsePresets(t *testing.T) {
	t.Parallel()

	t.Run("it reads a page of widgets in rows", func(t *testing.T) {
		t.Parallel()
		pages, err := parsePresets(presetFixture(
			presetWidgetJSON("a", 3, presetCount, presetNumber),
			presetWidgetJSON("b", 6, presetCount, presetNumber),
		))
		require.NoError(t, err)
		require.Len(t, pages["home"].Rows[0].Widgets, 2)
		require.Equal(t, 6, pages["home"].Rows[0].Widgets[1].Span)
	})

	for name, tc := range map[string]struct {
		raw  []byte
		want string
	}{
		"an unknown key": {
			raw:  []byte(`{"pages":[],"extra":true}`),
			want: `unknown field "extra"`,
		},
		"a span off the grid": {
			raw:  presetFixture(presetWidgetJSON("a", 5, presetCount, presetNumber)),
			want: "spans 5 columns",
		},
		"a row wider than the grid": {
			raw:  presetFixture(presetWidgetJSON("a", 6, presetCount, presetNumber), presetWidgetJSON("b", 12, presetCount, presetNumber)),
			want: "spans 18 columns",
		},
		"a key declared twice": {
			raw:  presetFixture(presetWidgetJSON("a", 3, presetCount, presetNumber), presetWidgetJSON("a", 3, presetCount, presetNumber)),
			want: "declared twice",
		},
		"a page declared twice": {
			raw:  []byte(`{"pages":[{"page":"home","rows":[{"widgets":[` + presetWidgetJSON("a", 3, presetCount, presetNumber) + `]}]},{"page":"home","rows":[]}]}`),
			want: `page "home" is declared twice`,
		},
		"a page with no rows": {
			raw:  []byte(`{"pages":[{"page":"home","rows":[]}]}`),
			want: "has no rows",
		},
		"a row with no widgets": {
			raw:  []byte(`{"pages":[{"page":"home","rows":[{"widgets":[]}]}]}`),
			want: "has no widgets",
		},
	} {
		t.Run("it refuses "+name, func(t *testing.T) {
			t.Parallel()
			_, err := parsePresets(tc.raw)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestPresetProblems(t *testing.T) {
	t.Parallel()

	pages, err := parsePresets(presetFixture(
		presetWidgetJSON("ok", 3, presetCount, presetNumber),
		presetWidgetJSON("gone", 3, `{"window":"24h","grain":"none","dimensions":["no_such_field"],"measures":[{"op":"count","alias":"count"}]}`, `{"type":"ranked","options":{}}`),
		presetWidgetJSON("undrawable", 3, presetCount, `{"type":"line","options":{}}`),
	))
	require.NoError(t, err)

	problems, err := presetProblems(analytics.Default, pages, time.Now())
	require.NoError(t, err)
	require.Len(t, problems, 2, "a field the catalog dropped and a chart that cannot draw its question both fail the gate")
	require.Contains(t, problems[0], `widget "gone"`)
	require.Contains(t, problems[1], `widget "undrawable"`)
}
