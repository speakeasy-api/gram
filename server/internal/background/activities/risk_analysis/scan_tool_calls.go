package risk_analysis

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// DescribeShadowMCP returns the canonical rule and description.
func DescribeShadowMCP(toolName string) (string, string) {
	if toolName == "" {
		return guard(RuleShadowMCP), "Detected an unverified MCP tool call."
	}
	return guard(RuleShadowMCP), fmt.Sprintf("Detected an unverified MCP tool call to %q.", toolName)
}

// DescribeDestructiveTool returns the canonical rule and description.
func DescribeDestructiveTool(toolName string) (string, string) {
	if toolName == "" {
		return guard(RuleDestructiveTool), "Detected a call to a tool annotated as destructive by its MCP server."
	}
	return guard(RuleDestructiveTool), fmt.Sprintf("Detected a call to %q, which its MCP server annotates as destructive.", toolName)
}

func (a *AnalyzeBatch) scanShadowMCP(ctx context.Context, orgID string, projectID uuid.UUID, messages []batchMessage) [][]Finding {
	out := make([][]Finding, len(messages))
	var deniedCallIDs []string
	for i, msg := range messages {
		findings, ids := a.scanMessageToolCalls(ctx, orgID, msg.ToolCalls)
		out[i] = findings
		deniedCallIDs = append(deniedCallIDs, ids...)
	}

	if len(deniedCallIDs) == 0 || a.mcpMatchLookup == nil {
		return out
	}

	matches, err := a.mcpMatchLookup.LookupMCPMatchesByToolCallID(ctx, projectID, deniedCallIDs)
	if err != nil {
		a.logger.WarnContext(ctx, "shadow_mcp scan: mcp match lookup failed; using server-prefix fallback", attr.SlogError(err))
		return out
	}
	for i := range out {
		for j := range out[i] {
			f := &out[i][j]
			if f.Source != shadowmcp.SourceShadowMCP {
				continue
			}
			if v, ok := matches[f.mcpLookupToolCallID]; ok && v != "" {
				f.Match = v
			}
		}
	}
	return out
}

// MCPMatchLookup resolves stored tool-call IDs to MCP match strings.
type MCPMatchLookup interface {
	LookupMCPMatchesByToolCallID(ctx context.Context, projectID uuid.UUID, toolCallIDs []string) (map[string]string, error)
}

func (a *AnalyzeBatch) scanMessageToolCalls(ctx context.Context, orgID string, calls []recordedToolCall) ([]Finding, []string) {
	var findings []Finding
	var deniedCallIDs []string
	for _, call := range calls {
		toolName := call.Function.Name
		if toolName == "" || !toolref.IsMCPToolName(toolName) {
			continue
		}
		toolInput := parseOptionalToolInput(call.Function.Arguments)
		bareName := toolref.MCPFunctionOf(toolName)
		if a.shadowMCPClient == nil {
			continue
		}
		_, denied := a.shadowMCPClient.ValidateToolsetCall(ctx, toolInput, bareName, orgID)
		if !denied {
			continue
		}
		match := toolref.MCPServerOf(toolName)
		if match == "" {
			match = toolName
		}
		ruleID, description := DescribeShadowMCP(toolName)
		findings = append(findings, Finding{
			Source:              shadowmcp.SourceShadowMCP,
			RuleID:              ruleID,
			Description:         description,
			Match:               match,
			StartPos:            0,
			EndPos:              0,
			Tags:                []string{},
			Confidence:          1.0,
			DeadLetterReason:    "",
			mcpLookupToolCallID: call.ID,
			spanGroupKey:        "",
			field:               "",
			path:                "",
		})
		if call.ID != "" {
			deniedCallIDs = append(deniedCallIDs, call.ID)
		}
	}
	return findings, deniedCallIDs
}

func (a *AnalyzeBatch) scanDestructiveToolAnnotations(ctx context.Context, orgID string, messages []batchMessage) [][]Finding {
	out := make([][]Finding, len(messages))
	for i, msg := range messages {
		out[i] = a.scanMessageDestructiveToolCalls(ctx, orgID, msg.ToolCalls)
	}
	return out
}

func (a *AnalyzeBatch) scanMessageDestructiveToolCalls(ctx context.Context, orgID string, calls []recordedToolCall) []Finding {
	if a.shadowMCPClient == nil {
		return nil
	}

	var findings []Finding
	for _, call := range calls {
		toolName := call.Function.Name
		if toolName == "" || !toolref.IsMCPToolName(toolName) {
			continue
		}

		toolInput, ok := parseRequiredToolInput(call.Function.Arguments)
		if !ok {
			continue
		}

		bareName := toolref.MCPFunctionOf(toolName)
		resolved, ok := a.shadowMCPClient.ResolveToolsetCall(ctx, toolInput, bareName, orgID)
		if !ok || resolved.Tool.Annotations == nil || resolved.Tool.Annotations.DestructiveHint == nil || !*resolved.Tool.Annotations.DestructiveHint {
			continue
		}

		ruleID, description := DescribeDestructiveTool(resolved.ToolName)
		findings = append(findings, Finding{
			Source:              shadowmcp.SourceDestructiveTool,
			RuleID:              ruleID,
			Description:         description,
			Match:               resolved.ToolName,
			StartPos:            0,
			EndPos:              0,
			Tags:                []string{},
			Confidence:          1.0,
			DeadLetterReason:    "",
			mcpLookupToolCallID: "",
			spanGroupKey:        "",
			field:               "",
			path:                "",
		})
	}
	return findings
}

func (a *AnalyzeBatch) scanDestructiveCLICommands(ctx context.Context, messages []batchMessage) [][]Finding {
	out := make([][]Finding, len(messages))
	for i, msg := range messages {
		out[i] = a.scanMessageDestructiveCLICalls(ctx, msg.ToolCalls)
	}
	return out
}

func (a *AnalyzeBatch) scanMessageDestructiveCLICalls(_ context.Context, calls []recordedToolCall) []Finding {
	var findings []Finding
	for _, call := range calls {
		toolName := call.Function.Name
		if toolName == "" {
			continue
		}

		matched, ok := scanForCLIDestructive(parseCLIToolInput(call.Function.Arguments))
		if !ok {
			continue
		}

		ruleID, description := DescribeCLIDestructive(matched, toolName)
		findings = append(findings, Finding{
			Source:              SourceCLIDestructive,
			RuleID:              ruleID,
			Description:         description,
			Match:               toolName,
			StartPos:            0,
			EndPos:              0,
			Tags:                []string{},
			Confidence:          1.0,
			DeadLetterReason:    "",
			mcpLookupToolCallID: "",
			spanGroupKey:        "",
			field:               "",
			path:                "",
		})
	}
	return findings
}

func parseOptionalToolInput(raw string) any {
	if raw == "" {
		return nil
	}
	var toolInput any
	if err := json.Unmarshal([]byte(raw), &toolInput); err != nil {
		return nil
	}
	return toolInput
}

func parseRequiredToolInput(raw string) (any, bool) {
	if raw == "" {
		return nil, true
	}
	var toolInput any
	if err := json.Unmarshal([]byte(raw), &toolInput); err != nil {
		return nil, false
	}
	return toolInput, true
}

func parseCLIToolInput(raw string) any {
	if raw == "" {
		return nil
	}
	var toolInput any
	if err := json.Unmarshal([]byte(raw), &toolInput); err != nil {
		return raw
	}
	return toolInput
}
