package core

import "github.com/google/uuid"

// AssistantsPlatformToolsetSlug is the reserved slug for the platform
// toolset granted to every assistant runtime.
const AssistantsPlatformToolsetSlug = "assistants"

// PlatformMCPReadToolsetSlug is the reserved slug for the platform toolset
// re-serving the Platform MCP read tools to a project's managed assistant.
// No other assistant is granted it.
const PlatformMCPReadToolsetSlug = "platform"

// SpeakeasySlackPlatformToolsetSlug is the reserved slug for the toolset a
// project's managed assistant replies through when the Speakeasy Slack app
// routes a workspace to its project. The tools post with that workspace's bot
// token, resolved on the server per call.
const SpeakeasySlackPlatformToolsetSlug = "speakeasy-slack"

// ResearchToolsetSlug is the reserved slug for the MCP research agent's web
// tools (search and page fetch). No assistant is granted it by default; the
// research-agent runner attaches it explicitly, and its tools are gated on
// the mcp_approval feature.
const ResearchToolsetSlug = "research"

var platformToolsetNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://app.getgram.ai/platform/mcp"))

// PlatformToolsetID returns the stable policy and finding identity for a
// code-owned Platform MCP toolset.
func PlatformToolsetID(slug string) uuid.UUID {
	return uuid.NewSHA1(platformToolsetNamespace, []byte(slug))
}

// PlatformToolsetName returns the dashboard label for a Platform MCP toolset.
func PlatformToolsetName(slug string) string {
	switch slug {
	case AssistantsPlatformToolsetSlug:
		return "Speakeasy assistant tools"
	case PlatformMCPReadToolsetSlug:
		return "Speakeasy Platform MCP"
	case SpeakeasySlackPlatformToolsetSlug:
		return "Speakeasy Slack app"
	default:
		return slug
	}
}

// AssistantBuiltinToolsetSlugs returns the built-in toolset slugs an
// assistant's backing agent receives mcp:connect access to.
func AssistantBuiltinToolsetSlugs(managed bool) []string {
	if managed {
		return []string{
			AssistantsPlatformToolsetSlug,
			PlatformMCPReadToolsetSlug,
			SpeakeasySlackPlatformToolsetSlug,
		}
	}
	return []string{AssistantsPlatformToolsetSlug}
}
