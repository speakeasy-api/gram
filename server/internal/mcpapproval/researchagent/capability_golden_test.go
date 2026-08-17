package researchagent_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/researchagent"
	"github.com/speakeasy-api/gram/server/internal/platformtools/research"
)

// This is the research agent's capability golden file. It pins exactly what
// the production runner can do from inside a loop whose caller — the model,
// after its first fetch — must be assumed hostile.
//
// If an edit lands here, it is changing the agent's security boundary, and
// the review must answer for the new capability under that assumption:
// egress-select tools take only parameters trusted code observed;
// egress-synthesize tools carry free-form model text and are registrable
// only toward recipients that already hold the run's full context. There is
// deliberately no capability class for reading tenant data (inputs belong in
// the briefing, compiled before the model reads anything untrusted) and none
// for writes (effects leave a run only through the validated report).
func TestProductionToolset_CapabilityGolden(t *testing.T) {
	t.Parallel()

	menu := research.NewURLMenu()
	registered := researchagent.ProductionToolset(
		research.NewWebSearchTool(research.NewSearchClient(nil), menu),
		research.NewFetchPageTool(research.ConfigureFetchClient(&http.Client{}), menu),
	)

	type entry struct {
		Name       string
		Capability researchagent.Capability
		Recipient  string
	}
	got := make([]entry, 0, len(registered))
	for _, tool := range registered {
		got = append(got, entry{Name: tool.Name(), Capability: tool.Capability(), Recipient: tool.Recipient()})
	}

	require.Equal(t, []entry{
		{
			Name:       "platform_web_search",
			Capability: researchagent.CapabilityEgressSynthesize,
			Recipient:  "OpenRouter and its search provider, which already receive the run's full context in every completion request",
		},
		{
			Name:       "platform_fetch_page",
			Capability: researchagent.CapabilityEgressSelect,
			Recipient:  "",
		},
	}, got)
}

// A synthesize-class registration that cannot name its recipient is refused
// at wiring time, before any run exists to be endangered by it.
func TestEgressSynthesizeTool_RequiresARecipient(t *testing.T) {
	t.Parallel()

	menu := research.NewURLMenu()
	search := research.NewWebSearchTool(research.NewSearchClient(nil), menu)
	require.Panics(t, func() {
		researchagent.EgressSynthesizeTool(search, "")
	})
}
