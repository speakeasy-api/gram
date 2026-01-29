package agents

import (
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// SpawnAgentToolName is the name of the tool used to spawn sub-agents
const SpawnAgentToolName = "spawn_agent"

// SpawnAgentArgs represents the arguments for spawning a sub-agent
type SpawnAgentArgs struct {
	// Name is a short descriptive name for the sub-agent
	Name string `json:"name"`
	// Task is the specific task the sub-agent should accomplish
	Task string `json:"task"`
	// Context provides additional information for the sub-agent
	Context string `json:"context,omitempty"`
	// ExecutionMode specifies how to execute relative to other spawn_agent calls
	// Valid values: "sequential", "parallel", "auto"
	ExecutionMode string `json:"execution_mode,omitempty"`
}

// SpawnAgentToolDefinition returns the tool definition for spawn_agent
func SpawnAgentToolDefinition() openrouter.Tool {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "A short descriptive name for the sub-agent (e.g., 'Research Agent', 'Code Reviewer')"
			},
			"task": {
				"type": "string",
				"description": "The specific task for the sub-agent to accomplish"
			},
			"context": {
				"type": "string",
				"description": "Additional context or information relevant to the task"
			},
			"execution_mode": {
				"type": "string",
				"enum": ["sequential", "parallel", "auto"],
				"description": "How to execute relative to other spawn_agent calls. 'sequential' waits for completion before starting next, 'parallel' runs concurrently, 'auto' lets the system decide based on dependencies"
			}
		},
		"required": ["name", "task"]
	}`)

	return openrouter.Tool{
		Type: "function",
		Function: &openrouter.FunctionDefinition{
			Name:        SpawnAgentToolName,
			Description: "Spawn a sub-agent to handle a specialized task. Use this when a task would benefit from focused, dedicated processing. The sub-agent will work independently and return its results.",
			Parameters:  schema,
		},
	}
}

// IsSpawnAgentTool checks if a tool call is for spawn_agent
func IsSpawnAgentTool(toolName string) bool {
	return toolName == SpawnAgentToolName
}

// ParseSpawnAgentArgs parses the arguments for a spawn_agent tool call
func ParseSpawnAgentArgs(argsJSON string) (*SpawnAgentArgs, error) {
	var args SpawnAgentArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("unmarshal spawn_agent args: %w", err)
	}
	return &args, nil
}
