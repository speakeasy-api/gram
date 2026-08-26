package metamcp

import "encoding/json"

// Tool is one entry of the fixed gateway tool contract as served by
// tools/list.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

var listServersSchema = json.RawMessage(`{
	"type": "object",
	"properties": {},
	"additionalProperties": false
}`)

var describeServerSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"server": {
			"type": "string",
			"description": "Slug of the member server to describe, as returned by list_servers."
		}
	},
	"required": ["server"],
	"additionalProperties": false
}`)

var describeToolsSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"tools": {
			"type": "array",
			"items": {
				"type": "string",
				"description": "Qualified tool name (serverslug--toolname) to fetch the full input schema for."
			},
			"description": "Qualified tool names to describe."
		}
	},
	"required": ["tools"],
	"additionalProperties": false
}`)

// Tools returns the fixed four-tool contract. executeToolSchema is supplied
// by the caller so the meta surface serves the exact execute_tool schema the
// dynamic toolset surface serves, without this package importing it.
func Tools(executeToolSchema json.RawMessage) []Tool {
	return []Tool{
		{
			Name:        ToolListServers,
			Description: "List the member MCP servers this gateway fronts: which systems are reachable, their ordering, and their connection state. Start here to orient before drilling into a specific server.",
			InputSchema: listServersSchema,
		},
		{
			Name:        ToolDescribeServer,
			Description: "Describe one member server's tool catalog: qualified tool names and descriptions, without input schemas. Call describe_tools for the schemas of the specific tools you intend to use.",
			InputSchema: describeServerSchema,
		},
		{
			Name:        ToolDescribeTools,
			Description: "Fetch full input schemas for named tools. Do not call a tool without first describing it to get its input schema. Member catalog failures are reported under failed rather than failing the whole call.",
			InputSchema: describeToolsSchema,
		},
		{
			Name:        ToolExecuteTool,
			Description: "Execute a specific tool by qualified name (serverslug--toolname), passing arguments that match that tool's schema.",
			InputSchema: executeToolSchema,
		},
	}
}
