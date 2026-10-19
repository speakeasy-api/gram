package dashboards

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
	"github.com/speakeasy-api/gram/server/internal/widgets"
)

// Every built-in dashboard is checked the way a saved one would be: each
// card validates against the catalog as a widget save does, sits within the
// grid at no less than its chart's minimum, and overlaps nothing. A card
// that fails here would fail the moment someone duplicated the dashboard.
func TestBuiltInDashboardsAreSound(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	slugs := map[string]bool{}
	for _, page := range builtIns {
		require.False(t, slugs[page.Slug], "slug %q is registered twice", page.Slug)
		slugs[page.Slug] = true
	}

	for _, page := range builtIns {
		t.Run(page.Slug, func(t *testing.T) {
			t.Parallel()
			require.Regexp(t, `^[a-z0-9]+(-[a-z0-9]+)*$`, page.Slug, "a slug is a URL path piece")
			require.NotEmpty(t, page.Name)
			require.NotEmpty(t, page.Description)
			require.LessOrEqual(t, utf8.RuneCountInString(page.Name), maxNameLength-utf8.RuneCountInString(copySuffix), "the copy is named the full name plus the suffix")
			require.NotEmpty(t, page.Cards)
			require.LessOrEqual(t, len(page.Cards), maxCards)

			laid := make([]placed, 0, len(page.Cards))
			for i, card := range page.Cards {
				position := fmt.Sprintf("cards[%d] %q", i, card.Name)
				require.NotEmpty(t, card.Name, position)
				require.LessOrEqual(t, utf8.RuneCountInString(card.Name), maxNameLength-utf8.RuneCountInString(copySuffix), position)

				var query, visualization map[string]any
				require.NoError(t, json.Unmarshal(card.Query, &query), "%s: query", position)
				require.NoError(t, json.Unmarshal(card.Visualization, &visualization), "%s: visualization", position)
				require.Equal(t, query, decodeCard(card.Query), position)

				reason, err := widgets.Validate(analytics.Default, card.Dataset, card.Query, card.Visualization, now)
				require.NoError(t, err, position)
				require.Empty(t, reason, "%s does not validate", position)

				input := &gen.PlacementInput{ID: "", WidgetID: "", X: card.X, Y: card.Y, W: card.W, H: card.H}
				require.Empty(t, checkPlacement(position, input, chartTypeOf(card.Visualization)))
				laid = append(laid, placed{position: position, input: input})
			}
			require.Empty(t, checkOverlaps(laid))
		})
	}
}

func TestBuiltInBySlug(t *testing.T) {
	t.Parallel()
	page, ok := builtInBySlug("mcp-tools")
	require.True(t, ok)
	require.Equal(t, "MCP & Tools", page.Name)
	_, ok = builtInBySlug("nothing")
	require.False(t, ok)
}

// The MCP & Tools page's cards, in order, so a card dropped by mistake is a
// failing test rather than a quieter page.
func TestMCPToolsCards(t *testing.T) {
	t.Parallel()
	names := make([]string, 0, len(mcpTools.Cards))
	for _, card := range mcpTools.Cards {
		names = append(names, card.Name)
		require.Equal(t, "tool_calls", card.Dataset, card.Name)
	}
	require.Equal(t, []string{
		"Tool calls",
		"Failures",
		"Calls over time",
		"Most used MCP servers",
		"Most used tools",
		"Most used clients",
		"Most errors",
		"Busiest people",
	}, names)

	views := builtInViews()
	require.Len(t, views, 1)
	require.Equal(t, "mcp-tools", views[0].Slug)
	require.Len(t, views[0].Cards, len(mcpTools.Cards))
	require.Equal(t, "number", views[0].Cards[0].Visualization["type"])
	require.Equal(t, "7d", views[0].Cards[0].Query["window"])
	require.Nil(t, views[0].Cards[0].Description, "a card with nothing more to say has no description")
	require.NotNil(t, views[0].Cards[1].Description)
}
