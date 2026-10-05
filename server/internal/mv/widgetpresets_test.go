package mv_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mv"
	widgetsrepo "github.com/speakeasy-api/gram/server/internal/widgets/repo"
)

func TestWidgetPresetObjectDecoding(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		query         string
		visualization string
		validator     string
		wantReason    string
	}{
		{
			name:          "preserves large integers",
			query:         `{"value":9007199254740993}`,
			visualization: `{"type":"number"}`,
		},
		{
			name:          "rejects null queries",
			query:         `null`,
			visualization: `{"type":"number"}`,
			wantReason:    "query is not a JSON object",
		},
		{
			name:          "rejects array visualizations",
			query:         `{}`,
			visualization: `[]`,
			wantReason:    "visualization is not a JSON object",
		},
		{
			name:          "rejects trailing input",
			query:         `{} {}`,
			visualization: `{"type":"number"}`,
			wantReason:    "unexpected trailing input",
		},
		{
			name:          "keeps the catalog failure",
			query:         `null`,
			visualization: `[]`,
			validator:     "unknown dataset",
			wantReason:    "unknown dataset",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			page := mv.PresetPageSource{
				Page: "home",
				Rows: []mv.PresetRowSource{{Widgets: []mv.PresetWidgetSource{{
					Key: "a", Name: "A", Dataset: "sessions", Span: 3,
					Query: json.RawMessage(tc.query), Visualization: json.RawMessage(tc.visualization),
				}}}},
			}
			view := mv.BuildWidgetPresetView(page, func(_ string, _, _ []byte) string { return tc.validator })
			preset := view.Rows[0].Widgets[0]
			saved := mv.BuildWidgetView(widgetsrepo.Widget{
				Query: []byte(tc.query), Visualization: []byte(tc.visualization),
			}, tc.validator)
			require.Equal(t, saved.Query, preset.Query)
			require.Equal(t, saved.Visualization, preset.Visualization)
			require.Equal(t, saved.InvalidReason, preset.InvalidReason)
			if tc.wantReason == "" {
				require.Nil(t, preset.InvalidReason)
				require.Equal(t, json.Number("9007199254740993"), preset.Query["value"])
			} else {
				require.NotNil(t, preset.InvalidReason)
				require.Contains(t, *preset.InvalidReason, tc.wantReason)
			}
		})
	}
}
