package model

import (
	"encoding/json"
	"slices"

	s "github.com/speakeasy-api/gram/spikes/clean-slate-authz/schema"
)

const CondMCPTools = "mcp_tools"

// MCPToolsParams restricts a connector grant (or block) to tools and/or dispositions.
type MCPToolsParams struct {
	Tools        []string `json:"tools,omitempty"`
	Dispositions []string `json:"dispositions,omitempty"`
}

// MCPToolContext is the fact a tool call supplies.
type MCPToolContext struct {
	Tool        string
	Disposition string
}

var MCPTools = s.NewCondition(CondMCPTools, func(p MCPToolsParams, c MCPToolContext) s.Tri {
	if len(p.Tools) > 0 {
		if c.Tool == "" {
			return s.Unknown
		}
		if !slices.Contains(p.Tools, c.Tool) {
			return s.False
		}
	}
	if len(p.Dispositions) > 0 {
		if c.Disposition == "" {
			return s.Unknown
		}
		if !slices.Contains(p.Dispositions, c.Disposition) {
			return s.False
		}
	}
	return s.True
})

func init() {
	MCPTools.Covers = func(parent, child json.RawMessage) bool {
		var p, c MCPToolsParams
		if json.Unmarshal(parent, &p) != nil || json.Unmarshal(child, &c) != nil {
			return false
		}
		return subset(c.Tools, p.Tools) && subset(c.Dispositions, p.Dispositions)
	}
}

// subset reports whether child is narrower than or equal to parent, where an
// empty list means "everything".
func subset(child, parent []string) bool {
	if len(parent) == 0 {
		return true
	}
	if len(child) == 0 {
		return false
	}
	for _, c := range child {
		if !slices.Contains(parent, c) {
			return false
		}
	}
	return true
}
