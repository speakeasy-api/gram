package gram

import (
	"github.com/jackc/pgx/v5/pgxpool"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	cursoragent "github.com/speakeasy-api/gram/server/internal/agentevents/cursor"
	chatmessagesink "github.com/speakeasy-api/gram/server/internal/agentevents/eventsink/sinks/chatmessage"
	telemetrysink "github.com/speakeasy-api/gram/server/internal/agentevents/eventsink/sinks/telemetry"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

func newAgentEvents(
	db *pgxpool.Pool,
	telemLogger *telemetry.Logger,
	chatWriter *chat.ChatMessageWriter,
	productFeatures *productfeatures.Client,
	chatTitleGenerator *background.TemporalChatTitleGenerator,
) (*agentevents.Mux, error) {
	mux := agentevents.NewMux()

	cursorAgent, err := buildCursorAgentHandler()
	if err != nil {
		return nil, err
	}
	if err := cursorAgent.Use(
		telemetrysink.New[*gen.CursorPayload](telemLogger),
		chatmessagesink.New[*gen.CursorPayload](chatmessagesink.Config{
			Writer:          chatWriter,
			ProductFeatures: productFeatures,
			DB:              db,
			TitleGenerator:  chatTitleGenerator,
		}),
	); err != nil {
		return nil, err
	}

	if err := mux.Register(cursorAgent, nil); err != nil {
		return nil, err
	}

	return mux, nil
}

func buildCursorAgentHandler() (*agentevents.Agent[*gen.CursorPayload], error) {
	cursorAgent, err := agentevents.NewAgent[*gen.CursorPayload](cursoragent.Agent)
	if err != nil {
		return nil, err
	}

	cursorAgent, err = cursorAgent.Builder().
		Register(
			agentevents.Resolve(types.FieldEventType, cursoragent.GetEventType),
			agentevents.Resolve(types.FieldHookSource, cursoragent.GetHookSource),
			agentevents.Resolve(types.FieldHookHostname, cursoragent.GetHookHostname),
			agentevents.Resolve(types.FieldBlockReason, cursoragent.GetBlockReason),
			agentevents.Resolve(types.FieldModel, cursoragent.GetModel),
		).
		RegisterFor([]types.EventType{types.AssistantResponseComplete, types.SessionEnded},
			agentevents.Resolve(types.FieldUsageInputTokens, cursoragent.GetUsageInputTokens),
			agentevents.Resolve(types.FieldUsageOutputTokens, cursoragent.GetUsageOutputTokens),
			agentevents.Resolve(types.FieldUsageCacheReadTokens, cursoragent.GetUsageCacheReadTokens),
			agentevents.Resolve(types.FieldUsageCacheWriteTokens, cursoragent.GetUsageCacheWriteTokens),
		).
		RegisterFor([]types.EventType{types.UserPromptSubmit, types.ToolCallStarted, types.MCPToolCallStarted},
			agentevents.Resolve(types.FieldScannableText, cursoragent.GetScannableText),
			agentevents.Resolve(types.FieldScanMessageType, cursoragent.GetScanMessageType),
		).
		RegisterFor([]types.EventType{
			types.ToolCallStarted,
			types.ToolCallCompleted,
			types.ToolCallFailed,
			types.MCPToolCallStarted,
			types.MCPToolCallCompleted,
		},
			agentevents.Resolve(types.FieldToolName, cursoragent.GetToolName),
			agentevents.Resolve(types.FieldToolDisplayName, cursoragent.GetToolDisplayName),
			agentevents.Resolve(types.FieldToolSource, cursoragent.GetToolSource),
			agentevents.Resolve(types.FieldToolInput, cursoragent.GetToolInput),
			agentevents.Resolve(types.FieldToolCallID, cursoragent.GetToolCallID),
		).
		RegisterFor([]types.EventType{types.ToolCallCompleted, types.ToolCallFailed, types.MCPToolCallCompleted},
			agentevents.Resolve(types.FieldToolOutput, cursoragent.GetToolOutput),
			agentevents.Resolve(types.FieldError, cursoragent.GetError),
		).
		RegisterFor([]types.EventType{types.UserPromptSubmit},
			agentevents.Resolve(types.FieldPrompt, cursoragent.GetPrompt),
		).
		RegisterFor([]types.EventType{types.AssistantResponseComplete},
			agentevents.Resolve(types.FieldAssistantText, cursoragent.GetAssistantText),
		).
		Build()
	if err != nil {
		return nil, err
	}

	return cursorAgent, nil
}
