package risk_analysis

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

func DescribeShadowMCP(toolName string) (string, string) {
	if toolName == "" {
		return guard(RuleShadowMCP), "Detected an unverified MCP tool call."
	}
	return guard(RuleShadowMCP), fmt.Sprintf("Detected an unverified MCP tool call to %q.", toolName)
}

func DescribeDestructiveTool(toolName string) (string, string) {
	if toolName == "" {
		return guard(RuleDestructiveTool), "Detected a call to a tool annotated as destructive by its MCP server."
	}
	return guard(RuleDestructiveTool), fmt.Sprintf("Detected a call to %q, which its MCP server annotates as destructive.", toolName)
}

func (a *AnalyzeBatch) scanShadowMCP(ctx context.Context, orgID string, projectID uuid.UUID, messages []repo.GetMessageContentBatchRow) [][]Finding {
	out := make([][]Finding, len(messages))
	var deniedCallIDs []string
	for i, msg := range messages {
		if len(msg.ToolCalls) == 0 {
			continue
		}
		findings, ids := a.scanMessageToolCalls(ctx, orgID, msg.ToolCalls)
		out[i] = findings
		deniedCallIDs = append(deniedCallIDs, ids...)
	}

	if len(deniedCallIDs) == 0 || a.mcpMatchLookup == nil {
		return out
	}

	matches, err := a.mcpMatchLookup.LookupMCPMatchesByToolCallID(ctx, projectID, deniedCallIDs)
	if err != nil {
		a.logger.WarnContext(ctx, "shadow_mcp scan: mcp match lookup failed; using server-prefix fallback",
			attr.SlogError(err),
		)
		return out
	}
	for i := range out {
		for j := range out[i] {
			f := &out[i][j]
			if f.Source != shadowmcp.SourceShadowMCP {
				continue
			}
			if v, ok := matches[f.toolCallID]; ok && v != "" {
				f.Match = v
			}
		}
	}
	return out
}

type recordedToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type MCPMatchLookup interface {
	LookupMCPMatchesByToolCallID(ctx context.Context, projectID uuid.UUID, toolCallIDs []string) (map[string]string, error)
}

func (a *AnalyzeBatch) parseRecordedToolCalls(ctx context.Context, source string, raw []byte) []recordedToolCall {
	var calls []recordedToolCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		a.logger.WarnContext(ctx, source+" scan: failed to parse tool_calls", attr.SlogError(err))
		return nil
	}
	return calls
}

func (a *AnalyzeBatch) scanMessageToolCalls(ctx context.Context, orgID string, raw []byte) ([]Finding, []string) {
	calls := a.parseRecordedToolCalls(ctx, shadowmcp.SourceShadowMCP, raw)

	var findings []Finding
	var deniedCallIDs []string
	for _, call := range calls {
		toolName := call.Function.Name
		if toolName == "" {
			continue
		}
		if !toolref.IsMCPToolName(toolName) {
			continue
		}
		var toolInput any
		if call.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &toolInput); err != nil {
				toolInput = nil
			}
		}
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
			Source:           shadowmcp.SourceShadowMCP,
			RuleID:           ruleID,
			Description:      description,
			Match:            match,
			StartPos:         0,
			EndPos:           0,
			Tags:             nil,
			Confidence:       1.0,
			DeadLetterReason: "",
			toolCallID:       call.ID,
			field:            "",
			path:             "",
		})
		if call.ID != "" {
			deniedCallIDs = append(deniedCallIDs, call.ID)
		}
	}
	return findings, deniedCallIDs
}

func (a *AnalyzeBatch) scanDestructiveToolAnnotations(ctx context.Context, orgID string, messages []repo.GetMessageContentBatchRow) [][]Finding {
	out := make([][]Finding, len(messages))
	for i, msg := range messages {
		if len(msg.ToolCalls) == 0 {
			continue
		}
		out[i] = a.scanMessageDestructiveToolCalls(ctx, orgID, msg.ToolCalls)
	}
	return out
}
func (a *AnalyzeBatch) scanMessageDestructiveToolCalls(ctx context.Context, orgID string, raw []byte) []Finding {
	if a.shadowMCPClient == nil {
		return nil
	}

	calls := a.parseRecordedToolCalls(ctx, shadowmcp.SourceDestructiveTool, raw)

	var findings []Finding
	for _, call := range calls {
		toolName := call.Function.Name
		if toolName == "" || !toolref.IsMCPToolName(toolName) {
			continue
		}

		var toolInput any
		if call.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &toolInput); err != nil {
				continue
			}
		}

		bareName := toolref.MCPFunctionOf(toolName)
		resolved, ok := a.shadowMCPClient.ResolveToolsetCall(ctx, toolInput, bareName, orgID)
		if !ok || resolved.Tool.Annotations == nil || resolved.Tool.Annotations.DestructiveHint == nil || !*resolved.Tool.Annotations.DestructiveHint {
			continue
		}

		ruleID, description := DescribeDestructiveTool(resolved.ToolName)
		findings = append(findings, Finding{
			Source:           shadowmcp.SourceDestructiveTool,
			RuleID:           ruleID,
			Description:      description,
			Match:            resolved.ToolName,
			StartPos:         0,
			EndPos:           0,
			Tags:             nil,
			Confidence:       1.0,
			DeadLetterReason: "",
			toolCallID:       "",
			field:            "",
			path:             "",
		})
	}
	return findings
}

func (a *AnalyzeBatch) scanDestructiveCLICommands(ctx context.Context, messages []repo.GetMessageContentBatchRow) [][]Finding {
	out := make([][]Finding, len(messages))
	for i, msg := range messages {
		if len(msg.ToolCalls) == 0 {
			continue
		}
		out[i] = a.scanMessageDestructiveCLICalls(ctx, msg.ToolCalls)
	}
	return out
}
func (a *AnalyzeBatch) scanMessageDestructiveCLICalls(ctx context.Context, raw []byte) []Finding {
	calls := a.parseRecordedToolCalls(ctx, SourceCLIDestructive, raw)

	var findings []Finding
	for _, call := range calls {
		toolName := call.Function.Name
		if toolName == "" {
			continue
		}

		var toolInput any
		if call.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &toolInput); err != nil {
				continue
			}
		}

		matched, ok := scanForCLIDestructive(toolInput)
		if !ok {
			continue
		}

		ruleID, description := DescribeCLIDestructive(matched, toolName)
		findings = append(findings, Finding{
			Source:           SourceCLIDestructive,
			RuleID:           ruleID,
			Description:      description,
			Match:            toolName,
			StartPos:         0,
			EndPos:           0,
			Tags:             nil,
			Confidence:       1.0,
			DeadLetterReason: "",
			toolCallID:       "",
			field:            "",
			path:             "",
		})
	}
	return findings
}
