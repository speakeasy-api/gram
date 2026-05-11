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
	AssistantMemoryTools []ExternalTool
}

type toolsetBuilder func(deps ToolsetDependencies) Toolset

var toolsetRegistry = []toolsetBuilder{
	func(deps ToolsetDependencies) Toolset {
		return NewAssistantsToolset(deps.AssistantMemoryTools...)
	},
}

// BuildToolsets materializes every registered platform toolset against the
// supplied dependencies. Callers wire it into the MCP service once at
// startup; adding a new toolset is a single registry entry and (optionally)
// a new dependency field.
func BuildToolsets(deps ToolsetDependencies) []Toolset {
	out := make([]Toolset, 0, len(toolsetRegistry))
	for _, b := range toolsetRegistry {
		out = append(out, b(deps))
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
	return base.JoinPath("x", "platform-mcp", slug).String()
}
