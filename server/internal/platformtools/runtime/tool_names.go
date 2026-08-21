package runtime

import (
	"slices"

	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

// The managed assistant's system prompt names its platform tools inline so the
// model can call them without discovery searches. These helpers return the
// names actually served, so a drift test can hold the prompt to the wiring
// instead of the prompt quietly rotting when a tool is added or renamed.
//
// Both build their tool slices with nil dependencies: a tool's Descriptor is
// static and never touches its service, so the names are exact while the
// executors are inert. Nothing here may be used to serve traffic.

// AssistantsToolsetToolNames returns the tool names on the base "assistants"
// platform toolset, granted to every assistant runtime.
func AssistantsToolsetToolNames() []string {
	tools := slices.Concat(
		MemoryExternalTools(nil),
		AssistantSkillTools(nil, nil, nil),
		TriggerExternalTools(nil, nil, nil),
	)
	return externalToolNames(tools)
}

// ManagedAssistantToolsetToolNames returns the tool names on the
// "managed-assistant" platform toolset — the legacy managed-only set of
// insights, chat, user, risk, deployment, skill, plugin, docs and changelog
// tools. Keep in lockstep with the managedInsightsTools wiring in start.go.
func ManagedAssistantToolsetToolNames() []string {
	tools := slices.Concat(
		ManagedAssistantLogsTools(nil),
		ManagedAssistantChatsTools(nil),
		ManagedAssistantUsersTools(nil),
		ManagedAssistantRiskTools(nil),
		ManagedAssistantDeploymentsTools(nil),
		ManagedAssistantSkillsTools(nil, nil),
		ManagedAssistantPluginsTools(nil),
		ManagedAssistantChangelogTools(nil),
		ManagedAssistantDocsTools(nil),
	)
	return externalToolNames(tools)
}

func externalToolNames(tools []platformtools.ExternalTool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Executor.Descriptor().Name)
	}
	slices.Sort(names)
	return names
}
