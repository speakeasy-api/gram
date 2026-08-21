package assistants

import (
	_ "embed"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

// The managed assistant's system prompt is per rollout variant, because the
// variant decides which platform toolset it is served and the two sets share
// no tool names. A prompt naming tools the runtime does not serve is worse
// than a thin one: the runner never advertises MCP tool schemas (the declared
// set is frozen for prompt-cache stability), so a model that trusts a stale
// name spends the turn searching the catalog for something that cannot be
// found.
//
// Both files name their tools inline, prefixed exactly as the runtime exposes
// them, so a turn needs no discovery search — only a batched `select:` search
// for input schemas. managed_instructions_test.go holds the inventories to the
// registries so a renamed or added tool fails the build rather than silently
// stranding the prompt.

//go:embed managed_assistant_instructions_legacy.txt
var managedAssistantInstructionsLegacy string

//go:embed managed_assistant_instructions_platformmcp.txt
var managedAssistantInstructionsPlatformMCP string

// managedAssistantInstructionsFor returns the system prompt for the managed
// assistant on the supplied rollout variant. Unrecognized variants resolve to
// legacy, matching feature.AssistantToolsVariant, so the prompt and the
// toolset can never disagree about which rollout the turn is on.
func managedAssistantInstructionsFor(variant feature.Variant) string {
	if feature.AssistantToolsVariant(variant) == feature.VariantAssistantToolsPlatformMCP {
		return managedAssistantInstructionsPlatformMCP
	}
	return managedAssistantInstructionsLegacy
}
