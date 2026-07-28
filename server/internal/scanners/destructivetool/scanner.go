// Package destructivetool is the single home for the destructive-tool-annotation
// scanner. It flags recorded Gram MCP tool calls whose resolved tool definition
// carries a destructive annotation (the MCP `destructiveHint`), converting each
// into the shared scanners.Finding domain type.
//
// Unlike clidestructive, which is content-driven, this scanner is
// annotation-driven: it resolves each MCP-routed call back to the Gram tool
// that produced it (via a Resolver, satisfied by *shadowmcp.Client) and reports
// a finding when that tool's server-declared annotations mark it destructive.
package destructivetool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// Source labels findings produced by this scanner.
const Source = shadowmcp.SourceDestructiveTool

// Rule is the canonical rule id emitted for every destructive_tool finding.
const Rule = "destructive.tool"

// Resolver resolves a recorded Gram MCP tool call back to its underlying tool
// definition. *shadowmcp.Client satisfies it. ok is false for missing
// provenance, unknown toolsets, and names not present in the resolved toolset.
type Resolver interface {
	ResolveToolsetCall(ctx context.Context, toolInput any, toolName string, orgID string) (*shadowmcp.ResolvedToolCall, bool)
}

// ToolCall is one recorded tool invocation to scan. Arguments is the raw JSON
// arguments string exactly as recorded on the tool call.
type ToolCall struct {
	Name      string
	Arguments string
}

// Scanner flags MCP tool calls whose resolved definition is annotated
// destructive. It is safe for concurrent use so long as the underlying
// Resolver is.
type Scanner struct {
	resolver Resolver
}

// NewScanner returns a Scanner backed by resolver, which must be non-nil.
func NewScanner(resolver Resolver) *Scanner {
	return &Scanner{resolver: resolver}
}

// Scan returns one Finding per MCP tool call whose resolved tool definition is
// annotated destructive. Non-MCP calls, nameless calls, and calls with
// malformed arguments are skipped, as are calls that fail to resolve or whose
// resolved tool is not annotated destructive.
func (s *Scanner) Scan(ctx context.Context, orgID string, calls []ToolCall) []scanners.Finding {
	var findings []scanners.Finding
	for _, call := range calls {
		if call.Name == "" || !toolref.IsMCPToolName(call.Name) {
			continue
		}

		toolInput, ok := parseToolInput(call.Arguments)
		if !ok {
			continue
		}

		bareName := toolref.MCPFunctionOf(call.Name)
		resolved, ok := s.resolver.ResolveToolsetCall(ctx, toolInput, bareName, orgID)
		if !ok || resolved.Tool.Annotations == nil || resolved.Tool.Annotations.DestructiveHint == nil || !*resolved.Tool.Annotations.DestructiveHint {
			continue
		}

		ruleID, description := describe(resolved.ToolName)
		findings = append(findings, scanners.Finding{
			Source:      Source,
			RuleID:      ruleID,
			Description: description,
			Match:       resolved.ToolName,
			StartPos:    0,
			EndPos:      0,
			Tags:        []string{},
			Confidence:  1.0,

			DeadLetterReason:    "",
			McpLookupToolCallID: "",
			SpanGroupKey:        "",
			Field:               "",
			Path:                "",
		})
	}
	return findings
}

// describe returns the canonical (rule_id, description) for a destructive_tool
// finding. The description never echoes tool arguments — only the tool name.
func describe(toolName string) (string, string) {
	if toolName == "" {
		return scanners.GuardRuleID(Rule), "Detected a call to a tool annotated as destructive by its MCP server."
	}
	return scanners.GuardRuleID(Rule), fmt.Sprintf("Detected a call to %q, which its MCP server annotates as destructive.", toolName)
}

// parseToolInput parses a recorded tool call's raw arguments string. Empty
// arguments are valid (ok=true, nil value); only malformed JSON returns
// ok=false so the caller skips the call rather than resolving with garbage.
func parseToolInput(raw string) (any, bool) {
	if raw == "" {
		return nil, true
	}
	var toolInput any
	if err := json.Unmarshal([]byte(raw), &toolInput); err != nil {
		return nil, false
	}
	return toolInput, true
}
