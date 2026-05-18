package platformtools

import "net/url"

// AssistantsPlatformToolsetSlug is the reserved slug for the platform
// toolset granted to every assistant runtime.
const AssistantsPlatformToolsetSlug = "assistants"

// Toolset is a virtual collection of platform tools exposed at runtime via a
// dedicated MCP endpoint. Platform toolsets are not persisted; the slug is
// hardcoded per consumer and wired in code at process startup.
type Toolset struct {
	Slug  string
	Tools []ExternalTool
}

// ToolsetDependencies bundles the inputs required to materialize the static
// platform toolset registry. Add a field here when a new toolset needs an
// external service or pre-built tool slice.
type ToolsetDependencies struct {
	AssistantMemoryTools  []ExternalTool
	AssistantTriggerTools []ExternalTool
}

type toolsetBuilder func(deps ToolsetDependencies) Toolset

var toolsetRegistry = []toolsetBuilder{
	func(deps ToolsetDependencies) Toolset {
		tools := make([]ExternalTool, 0, len(deps.AssistantMemoryTools)+len(deps.AssistantTriggerTools))
		tools = append(tools, deps.AssistantMemoryTools...)
		tools = append(tools, deps.AssistantTriggerTools...)
		return NewAssistantsToolset(tools...)
	},
}

// BuildToolsets materializes every registered platform toolset against the
// supplied dependencies and returns them indexed by slug. Panics on
// misconfiguration (empty slug or duplicate slug) so registry mistakes
// surface at startup instead of as runtime 404s.
func BuildToolsets(deps ToolsetDependencies) map[string]Toolset {
	out := make(map[string]Toolset, len(toolsetRegistry))
	for _, b := range toolsetRegistry {
		ts := b(deps)
		if ts.Slug == "" {
			panic("platformtools: registered toolset has empty slug")
		}
		if _, dup := out[ts.Slug]; dup {
			panic("platformtools: duplicate toolset slug " + ts.Slug)
		}
		out[ts.Slug] = ts
	}
	return out
}

// NewAssistantsToolset returns the assistants platform toolset bound to the
// supplied tools. Exposed for tests and direct callers; production wiring
// goes through BuildToolsets.
func NewAssistantsToolset(tools ...ExternalTool) Toolset {
	return Toolset{Slug: AssistantsPlatformToolsetSlug, Tools: tools}
}

// PlatformToolsetURL builds the URL where a runtime reaches the named
// platform toolset. Keep the path segments in lockstep with the chi route
// registered by the mcp package.
func PlatformToolsetURL(base *url.URL, slug string) string {
	return base.JoinPath("platform", "mcp", slug).String()
}
