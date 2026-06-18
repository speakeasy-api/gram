package cursor

import (
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

const Agent types.Provider = "cursor"

func NewAgent() (*agentevents.Agent[*gen.CursorPayload], error) {
	agent, err := agentevents.NewAgent[*gen.CursorPayload](Agent)
	if err != nil {
		return nil, err
	}

	if err := agent.Register(agentevents.Resolve(types.FieldEventType, GetEventType)); err != nil {
		return nil, err
	}
	if err := agent.RegisterFor(
		[]types.EventType{types.UserPromptSubmit, types.ToolCallStarted, types.MCPToolCallStarted},
		agentevents.Resolve(types.FieldScannableText, GetScannableText),
		agentevents.Resolve(types.FieldScanMessageType, GetScanMessageType),
		agentevents.Resolve(types.FieldToolName, GetToolName),
	); err != nil {
		return nil, err
	}

	return agent, nil
}
