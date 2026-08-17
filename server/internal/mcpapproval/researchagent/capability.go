package researchagent

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	platformresearch "github.com/speakeasy-api/gram/server/internal/platformtools/research"
)

// Capability classifies what a registered tool can do from inside the agent
// loop, where the model calling it must be assumed hostile after its first
// fetch. The set is closed on purpose: there is no capability for reading
// tenant data — inputs reach the agent through the briefing, compiled by
// trusted code before the model has read anything untrusted — and none for
// writes, because effects leave a run only through the validated report. A
// tool that needs either cannot be expressed here, so it cannot be
// registered; extending this enum is a change to the security boundary, not
// to a feature.
type Capability string

const (
	// CapabilityEgressSelect marks a tool whose attacker-visible parameters
	// are selections from what trusted code observed — the URL menu — never
	// values the model composed. A compromised model gains reach through such
	// a tool but no channel: it cannot address a byte of its context anywhere
	// trusted code did not already point.
	CapabilityEgressSelect Capability = "egress_select"

	// CapabilityEgressSynthesize marks a tool whose parameters are free-form
	// model text. Registration must name the recipient and that recipient
	// must already hold the run's full context (today: the completion
	// provider, which receives the whole prompt on every turn) — synthesized
	// parameters to such a recipient cross no trust boundary the run has not
	// already crossed.
	CapabilityEgressSynthesize Capability = "egress_synthesize"
)

// RegisteredTool binds one executor to its declared capability. Values are
// built only through EgressSelectTool and EgressSynthesizeTool; there is no
// literal form with meaning, so the constructors are the whole registration
// surface.
type RegisteredTool struct {
	executor   core.PlatformToolExecutor
	capability Capability

	// recipient names who receives synthesized parameters, and the doc-level
	// argument for why that recipient already holds the run's context. Empty
	// for select-class tools, whose parameters carry nothing composable.
	recipient string
}

// Name reports the registered executor's tool name.
func (t RegisteredTool) Name() string {
	return t.executor.Descriptor().Name
}

// Capability reports the declared capability class.
func (t RegisteredTool) Capability() Capability {
	return t.capability
}

// Recipient reports who receives synthesized parameters, empty for
// select-class tools.
func (t RegisteredTool) Recipient() string {
	return t.recipient
}

// EgressSelectTool registers a tool whose parameters are selections from the
// trusted URL menu.
func EgressSelectTool(tool core.PlatformToolExecutor) RegisteredTool {
	return RegisteredTool{executor: tool, capability: CapabilityEgressSelect, recipient: ""}
}

// EgressSynthesizeTool registers a tool with free-form parameters. The
// recipient argument is mandatory documentation of who receives them; a
// registration that cannot say so is refused by panic at wiring time, before
// any run exists.
func EgressSynthesizeTool(tool core.PlatformToolExecutor, recipient string) RegisteredTool {
	if recipient == "" {
		panic(fmt.Sprintf("registering synthesize-class tool %q requires naming the recipient of its parameters", tool.Descriptor().Name))
	}
	return RegisteredTool{executor: tool, capability: CapabilityEgressSynthesize, recipient: recipient}
}

// ProductionToolset is the one tool registration the research runner ships
// with. It exists as a function — rather than inline wiring — so the golden
// capability test can pin exactly what the agent holds; growing this set is
// a security-boundary change and the test failing is the point.
func ProductionToolset(search *platformresearch.WebSearch, fetch *platformresearch.FetchPage) []RegisteredTool {
	return []RegisteredTool{
		EgressSynthesizeTool(search, "OpenRouter and its search provider, which already receive the run's full context in every completion request"),
		EgressSelectTool(fetch),
	}
}
