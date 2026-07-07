package risk_analysis

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/scanners/destructivetool"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
)

// MCPMatchLookup resolves stored tool-call IDs to MCP match strings. It is the
// risk_analysis-facing name for shadowmcpscan.MatchLookup, kept here because it
// is part of the NewAnalyzeBatch constructor signature.
type MCPMatchLookup interface {
	LookupMCPMatchesByToolCallID(ctx context.Context, projectID uuid.UUID, toolCallIDs []string) (map[string]string, error)
}

func (a *AnalyzeBatch) scanShadowMCP(ctx context.Context, orgID string, projectID uuid.UUID, messages []batchMessage) [][]scanners.Finding {
	calls := make([][]shadowmcpscan.ToolCall, len(messages))
	for i, msg := range messages {
		msgCalls := make([]shadowmcpscan.ToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			msgCalls = append(msgCalls, shadowmcpscan.ToolCall{
				ID:        call.ID,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			})
		}
		calls[i] = msgCalls
	}
	return a.shadowMCPScanner.Scan(ctx, orgID, projectID, calls)
}

func (a *AnalyzeBatch) scanDestructiveToolAnnotations(ctx context.Context, orgID string, messages []batchMessage) [][]scanners.Finding {
	out := make([][]scanners.Finding, len(messages))
	for i, msg := range messages {
		calls := make([]destructivetool.ToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			calls = append(calls, destructivetool.ToolCall{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			})
		}
		out[i] = a.destructiveToolScanner.Scan(ctx, orgID, calls)
	}
	return out
}

func (a *AnalyzeBatch) scanDestructiveCLICommands(_ context.Context, messages []batchMessage) [][]scanners.Finding {
	out := make([][]scanners.Finding, len(messages))
	for i, msg := range messages {
		calls := make([]clidestructive.ToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			calls = append(calls, clidestructive.ToolCall{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			})
		}
		out[i] = a.cliDestructiveScanner.Scan(calls)
	}
	return out
}
