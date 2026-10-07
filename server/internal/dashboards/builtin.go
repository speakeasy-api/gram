package dashboards

import (
	"encoding/json"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
)

// A built-in dashboard ships with the product: its layout is this code, the
// same in every project, and nothing in the database refers to it. It is
// read only. Duplicating it makes a project dashboard with a saved widget
// per card, which can then be changed like any other.
//
// Each card holds the shape the widgets service stores for a saved widget
// (the client's widgetFromSpec writes it), as JSON text rather than Go
// values: the client's test parses the cards out of this file and proves
// each opens in Explore exactly as the card drew it, and the Go test proves
// each validates against the catalog and fits the grid. Keep every query
// and visualization a literal for that reason.
type builtIn struct {
	// Slug names the dashboard in links and when duplicating it; it is
	// URL-safe and never changes once shipped.
	Slug        string
	Name        string
	Description string
	Cards       []builtInCard
}

// builtInCard is one card: a widget's question and drawing, and where it
// sits on the 12-column grid.
type builtInCard struct {
	Name          string
	Description   string
	Dataset       string
	Query         json.RawMessage
	Visualization json.RawMessage
	X, Y, W, H    int
}

// builtIns is every built-in dashboard, in the order the Dashboards tab
// lists them.
var builtIns = []builtIn{mcpTools}

// builtInBySlug finds a built-in dashboard by its slug.
func builtInBySlug(slug string) (builtIn, bool) {
	for _, page := range builtIns {
		if page.Slug == slug {
			return page, true
		}
	}
	return builtIn{Slug: "", Name: "", Description: "", Cards: nil}, false
}

// builtInViews renders every built-in dashboard for the list.
func builtInViews() []*gen.BuiltInDashboard {
	out := make([]*gen.BuiltInDashboard, 0, len(builtIns))
	for _, page := range builtIns {
		out = append(out, page.view())
	}
	return out
}

func (b builtIn) view() *gen.BuiltInDashboard {
	cards := make([]*gen.BuiltInCard, 0, len(b.Cards))
	for _, card := range b.Cards {
		cards = append(cards, card.view())
	}
	return &gen.BuiltInDashboard{Slug: b.Slug, Name: b.Name, Description: b.Description, Cards: cards}
}

func (c builtInCard) view() *gen.BuiltInCard {
	var description *string
	if c.Description != "" {
		description = &c.Description
	}
	return &gen.BuiltInCard{
		Name:          c.Name,
		Description:   description,
		Dataset:       c.Dataset,
		Query:         decodeCard(c.Query),
		Visualization: decodeCard(c.Visualization),
		X:             c.X,
		Y:             c.Y,
		W:             c.W,
		H:             c.H,
	}
}

// decodeCard reads a card's JSON literal as the map the API carries. The
// registry test proves every literal decodes, so a failure here is a card
// the test did not see; it renders empty rather than failing the whole
// list.
func decodeCard(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// mcpTools is the MCP & Tools page as a dashboard: what the agents called,
// how often it failed, and who was calling. Every card asks the tool_calls
// dataset over the past seven days, the window the page's filter bar opens
// on.
//
// Three cards of the designed layout wait on catalog work and are not
// registered yet: "Tools used" and "People", two number tiles counting the
// distinct tool_name and user values (DNO-1271, count_distinct), which take
// the right half of the top row at (6,0,3,2) and (9,0,3,2) once the two
// tiles here narrow to 3 columns each; and "Most used skills", a ranking by
// the skill dimension (DNO-1272), which takes (4,10,4,4) once the two
// rankings on the bottom row narrow to 4 columns each.
var mcpTools = builtIn{
	Slug:        "mcp-tools",
	Name:        "MCP & Tools",
	Description: "Which MCP servers and tools the agents call, how often a call fails, and who is calling.",
	Cards: []builtInCard{
		{
			Name:          "Tool calls",
			Description:   "",
			Dataset:       "tool_calls",
			Query:         json.RawMessage(`{"window":"7d","grain":"none","ungrouped":false,"dimensions":[],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[],"order_by":[],"limit":0}`),
			Visualization: json.RawMessage(`{"type":"number","options":{}}`),
			X:             0, Y: 0, W: 6, H: 2,
		},
		{
			Name:          "Failures",
			Description:   "Calls that ended in an error",
			Dataset:       "tool_calls",
			Query:         json.RawMessage(`{"window":"7d","grain":"none","ungrouped":false,"dimensions":[],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[{"field":"status","operator":"in","values":["error"]}],"order_by":[],"limit":0}`),
			Visualization: json.RawMessage(`{"type":"number","options":{}}`),
			X:             6, Y: 0, W: 6, H: 2,
		},
		{
			Name:          "Calls over time",
			Description:   "Which servers the calls went to, day by day",
			Dataset:       "tool_calls",
			Query:         json.RawMessage(`{"window":"7d","grain":"day","ungrouped":false,"dimensions":["mcp_server"],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[],"order_by":[],"limit":1000}`),
			Visualization: json.RawMessage(`{"type":"stacked_bar","options":{}}`),
			X:             0, Y: 2, W: 12, H: 4,
		},
		{
			Name:          "Most used MCP servers",
			Description:   "",
			Dataset:       "tool_calls",
			Query:         json.RawMessage(`{"window":"7d","grain":"none","ungrouped":false,"dimensions":["mcp_server"],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[],"order_by":[{"measure":"count","direction":"desc"}],"limit":10}`),
			Visualization: json.RawMessage(`{"type":"ranked","options":{}}`),
			X:             0, Y: 6, W: 4, H: 4,
		},
		{
			Name:          "Most used tools",
			Description:   "",
			Dataset:       "tool_calls",
			Query:         json.RawMessage(`{"window":"7d","grain":"none","ungrouped":false,"dimensions":["tool_name"],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[],"order_by":[{"measure":"count","direction":"desc"}],"limit":10}`),
			Visualization: json.RawMessage(`{"type":"ranked","options":{}}`),
			X:             4, Y: 6, W: 4, H: 4,
		},
		{
			Name:          "Most used clients",
			Description:   "The agents the calls came from",
			Dataset:       "tool_calls",
			Query:         json.RawMessage(`{"window":"7d","grain":"none","ungrouped":false,"dimensions":["surface"],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[],"order_by":[{"measure":"count","direction":"desc"}],"limit":10}`),
			Visualization: json.RawMessage(`{"type":"ranked","options":{}}`),
			X:             8, Y: 6, W: 4, H: 4,
		},
		{
			Name:          "Most errors",
			Description:   "The tools whose calls failed most",
			Dataset:       "tool_calls",
			Query:         json.RawMessage(`{"window":"7d","grain":"none","ungrouped":false,"dimensions":["tool_name"],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[{"field":"status","operator":"in","values":["error"]}],"order_by":[{"measure":"count","direction":"desc"}],"limit":10}`),
			Visualization: json.RawMessage(`{"type":"ranked","options":{}}`),
			X:             0, Y: 10, W: 6, H: 4,
		},
		{
			Name:          "Busiest people",
			Description:   "",
			Dataset:       "tool_calls",
			Query:         json.RawMessage(`{"window":"7d","grain":"none","ungrouped":false,"dimensions":["user"],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[],"order_by":[{"measure":"count","direction":"desc"}],"limit":10}`),
			Visualization: json.RawMessage(`{"type":"ranked","options":{}}`),
			X:             6, Y: 10, W: 6, H: 4,
		},
	},
}
