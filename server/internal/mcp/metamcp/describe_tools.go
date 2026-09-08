package metamcp

import "encoding/json"

// SchemaTool is one member tool as reported by describe_tools: the qualified
// name with the full input schema an agent needs before calling it.
type SchemaTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Annotations any             `json:"annotations,omitempty"`
}

// DescribeToolsResult is the structuredContent payload of a describe_tools
// call. NotFound lists requested qualified names that resolved to no live
// tool — reported explicitly rather than silently dropped, so an agent
// holding a stale catalog knows to rediscover.
type DescribeToolsResult struct {
	Tools    []SchemaTool   `json:"tools"`
	NotFound []string       `json:"not_found,omitempty"`
	Failed   []FailedServer `json:"failed,omitempty"`
}

// FailedServer is one member whose catalog could not be read; its names are
// reported here so a member outage degrades only that member.
type FailedServer struct {
	Server  string `json:"server"`
	Message string `json:"message"`
}
