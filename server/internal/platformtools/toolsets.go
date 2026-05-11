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

func NewAssistantsToolset(tools ...ExternalTool) Toolset {
	return Toolset{Slug: AssistantsPlatformToolsetSlug, Tools: tools}
}

// PlatformToolsetURL builds the URL where a runtime reaches the named
// platform toolset. Keep the path segments in lockstep with the chi route
// registered by the mcp package.
func PlatformToolsetURL(base *url.URL, slug string) string {
	return base.JoinPath("x", "platform-mcp", slug).String()
}
