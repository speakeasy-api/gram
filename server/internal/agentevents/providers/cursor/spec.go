package cursor

import (
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

func Spec() agentevents.Spec[*gen.CursorPayload] {
	return agentevents.Spec[*gen.CursorPayload]{
		Provider: Agent,
		Build: func(b *agentevents.AgentBuilder[*gen.CursorPayload]) *agentevents.AgentBuilder[*gen.CursorPayload] {
			return b.
				Register(
					agentevents.Resolve(types.FieldEventType, GetEventType),
					agentevents.Resolve(types.FieldHookHostname, GetHookHostname),
					agentevents.Resolve(types.FieldModel, GetModel),
				).
				RegisterFor([]types.EventType{types.AssistantResponseComplete, types.SessionEnded},
					agentevents.Resolve(types.FieldUsageInputTokens, GetUsageInputTokens),
					agentevents.Resolve(types.FieldUsageOutputTokens, GetUsageOutputTokens),
					agentevents.Resolve(types.FieldUsageCacheReadTokens, GetUsageCacheReadTokens),
					agentevents.Resolve(types.FieldUsageCacheWriteTokens, GetUsageCacheWriteTokens),
				).
				RegisterFor([]types.EventType{types.UserPromptSubmit, types.ToolCallStarted, types.MCPToolCallStarted},
					agentevents.Resolve(types.FieldScannableText, GetScannableText),
					agentevents.Resolve(types.FieldScanMessageType, GetScanMessageType),
				).
				RegisterFor([]types.EventType{
					types.ToolCallStarted,
					types.ToolCallCompleted,
					types.ToolCallFailed,
					types.MCPToolCallStarted,
					types.MCPToolCallCompleted,
				},
					agentevents.Resolve(types.FieldToolName, GetToolName),
					agentevents.Resolve(types.FieldToolSource, GetToolSource),
					agentevents.Resolve(types.FieldToolInput, GetToolInput),
					agentevents.Resolve(types.FieldToolCallID, GetToolCallID),
				).
				RegisterFor([]types.EventType{types.ToolCallCompleted, types.ToolCallFailed, types.MCPToolCallCompleted},
					agentevents.Resolve(types.FieldToolOutput, GetToolOutput),
					agentevents.Resolve(types.FieldError, GetError),
				).
				RegisterFor([]types.EventType{types.UserPromptSubmit},
					agentevents.Resolve(types.FieldPrompt, GetPrompt),
				).
				RegisterFor([]types.EventType{types.AssistantResponseComplete},
					agentevents.Resolve(types.FieldAssistantText, GetAssistantText),
				)
		},
	}
}
