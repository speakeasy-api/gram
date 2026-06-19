package cursor

import (
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

const Agent types.Provider = "cursor"

func NewAgent() (*agentevents.Agent[*gen.CursorPayload], error) {
	return agentevents.NewAgent(
		Agent,
		agentevents.Register(agentevents.Resolve(types.FieldEventType, GetEventType)),
		agentevents.RegisterFor(
			[]types.EventType{types.UserPromptSubmit},
			agentevents.Resolve(types.FieldPrompt, GetPrompt),
		),
		agentevents.RegisterFor(
			[]types.EventType{types.BeforeToolUse, types.BeforeMCPExecution},
			agentevents.Resolve(types.FieldToolName, GetToolName),
			agentevents.Resolve(types.FieldToolInput, GetToolInput),
		),
		agentevents.RegisterFor(
			[]types.EventType{types.AfterToolUse},
			agentevents.Resolve(types.FieldToolName, GetToolName),
			agentevents.Resolve(types.FieldToolOutput, GetToolOutput),
		),
	)
}
