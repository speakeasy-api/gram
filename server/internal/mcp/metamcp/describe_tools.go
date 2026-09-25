package metamcp

import (
	"encoding/json"
	"fmt"
)

// SchemaTool is one member tool as reported by describe_tools: the qualified
// name with the full input schema an agent needs before calling it.
type SchemaTool struct {
	RawDefinition json.RawMessage `json:"-"`
	Title         string          `json:"title,omitempty"`
	OutputSchema  json.RawMessage `json:"outputSchema,omitempty"`
	Icons         json.RawMessage `json:"icons,omitempty"`
	Execution     json.RawMessage `json:"execution,omitempty"`
	Meta          map[string]any  `json:"_meta,omitempty"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	InputSchema   json.RawMessage `json:"inputSchema,omitempty"`
	Annotations   any             `json:"annotations,omitempty"`
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

// MarshalJSON preserves upstream extension fields when qualifying a tool name.
func (t SchemaTool) MarshalJSON() ([]byte, error) {
	type wire SchemaTool
	if len(t.RawDefinition) == 0 {
		raw, err := json.Marshal(wire(t))
		if err != nil {
			return nil, fmt.Errorf("encode described tool: %w", err)
		}
		return raw, nil
	}
	return MarshalToolDefinition(t.RawDefinition, t.Name)
}

// MarshalToolDefinition changes only the name, retaining every upstream field.
func MarshalToolDefinition(rawDefinition json.RawMessage, qualifiedName string) ([]byte, error) {
	var definition map[string]json.RawMessage
	if err := json.Unmarshal(rawDefinition, &definition); err != nil {
		return nil, fmt.Errorf("decode described tool: %w", err)
	}
	if definition == nil {
		return nil, fmt.Errorf("tool definition must be an object")
	}
	name, err := json.Marshal(qualifiedName)
	if err != nil {
		return nil, fmt.Errorf("encode qualified name: %w", err)
	}
	definition["name"] = name
	raw, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("encode full described tool: %w", err)
	}
	return raw, nil
}
