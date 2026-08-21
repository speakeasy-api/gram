package platformtools

import "net/url"

// AssistantsPlatformToolsetSlug is the reserved slug for the platform
// toolset granted to every assistant runtime.
const AssistantsPlatformToolsetSlug = "assistants"

// ManagedAssistantPlatformToolsetSlug is the reserved slug for the platform
// toolset granted only to a project's managed assistant (the one powering the
// dashboard sidebar). It carries tools that must not be reachable by any
// other assistant.
const ManagedAssistantPlatformToolsetSlug = "managed-assistant"

// PlatformMCPReadToolsetSlug is the reserved slug for the platform toolset
// re-serving the Platform MCP read tools to a project's managed assistant.
// Granted only when the assistant-platform-mcp rollout flag is on for the
// organization.
const PlatformMCPReadToolsetSlug = "platform"

// ResearchToolsetSlug is the reserved slug for the MCP research agent's web
// tools (search and page fetch). No assistant is granted it by default; the
// research-agent runner attaches it explicitly, and its tools are gated on
// the mcp_approval feature.
const ResearchToolsetSlug = "research"

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
	// AssistantTools and ManagedAssistantInsightsTools each arrive composed,
	// from one function in platformtools/runtime, so the served set has a
	// single statement. The managed assistant's system prompt names these
	// tools inline and a drift test holds it to those compositions; splitting
	// a toolset across several fields here would give the test a mirror to
	// check rather than the thing being served.
	AssistantTools                []ExternalTool
	ManagedAssistantInsightsTools []ExternalTool
	PlatformMCPReadTools          []ExternalTool
}

type toolsetBuilder func(deps ToolsetDependencies) Toolset

var toolsetRegistry = []toolsetBuilder{
	func(deps ToolsetDependencies) Toolset {
		return NewAssistantsToolset(deps.AssistantTools...)
	},
	func(deps ToolsetDependencies) Toolset {
		tools := make([]ExternalTool, 0, len(deps.ManagedAssistantInsightsTools))
		tools = append(tools, deps.ManagedAssistantInsightsTools...)
		return NewManagedAssistantToolset(tools...)
	},
	func(deps ToolsetDependencies) Toolset {
		tools := make([]ExternalTool, 0, len(deps.PlatformMCPReadTools))
		tools = append(tools, deps.PlatformMCPReadTools...)
		return NewPlatformMCPReadToolset(tools...)
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

// NewManagedAssistantToolset returns the platform toolset granted only to a
// project's managed assistant. Exposed for tests and direct callers;
// production wiring goes through BuildToolsets.
func NewManagedAssistantToolset(tools ...ExternalTool) Toolset {
	return Toolset{Slug: ManagedAssistantPlatformToolsetSlug, Tools: tools}
}

// NewPlatformMCPReadToolset returns the platform toolset re-serving the
// Platform MCP read tools to a project's managed assistant. Exposed for tests
// and direct callers; production wiring goes through BuildToolsets.
func NewPlatformMCPReadToolset(tools ...ExternalTool) Toolset {
	return Toolset{Slug: PlatformMCPReadToolsetSlug, Tools: tools}
}

// PlatformToolsetURL builds the URL where a runtime reaches the named
// platform toolset. Keep the path segments in lockstep with the chi route
// registered by the mcp package.
func PlatformToolsetURL(base *url.URL, slug string) string {
	return base.JoinPath("platform", "mcp", slug).String()
}
