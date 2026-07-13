package hooks

// Transcript-derived MCP attribution intake. Claude redacts user-configured
// MCP server/tool names to "custom" on its OTEL api_request telemetry, but
// records the real names in the local session transcript. The Claude plugin's
// Stop/SubagentStop hooks extract (request_id, mcp_server, mcp_tool) tuples
// from the transcript and ship them on the unified ingest payload
// (data.mcp_attribution). This file stores those tuples in Redis, keyed per
// request id; the scheduled staged-telemetry sweep joins them against the
// rows parked in telemetry_logs_staging (see server/internal/hooks/otel.go
// for the staging fork and
// server/internal/background/promote_staged_telemetry.go for promotion).

import (
	"context"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// captureMCPAttribution stores every attribution tuple the payload carries;
// the scheduled sweep picks them up within its two-minute interval. Failures
// are non-fatal: a missed tuple only means the affected staged rows promote
// verbatim (still "custom") after the timeout — today's behavior.
func (s *Service) captureMCPAttribution(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext) {
	if payload.Data == nil || len(payload.Data.McpAttribution) == 0 || authCtx.ProjectID == nil {
		return
	}

	sessionID := canonicalSessionID(payload)
	for _, entry := range payload.Data.McpAttribution {
		if entry == nil {
			continue
		}
		requestID := strings.TrimSpace(entry.RequestID)
		server := strings.TrimSpace(conv.PtrValOr(entry.McpServer, ""))
		if requestID == "" || server == "" {
			continue
		}
		tuple := telemetry.MCPAttributionTuple{
			Server: server,
			Tool:   strings.TrimSpace(conv.PtrValOr(entry.McpTool, "")),
		}
		if err := s.cache.Set(ctx, telemetry.MCPAttributionTupleKey(authCtx.ProjectID.String(), requestID), tuple, telemetry.MCPAttributionTupleTTL); err != nil {
			s.logger.WarnContext(ctx, "failed to store MCP attribution tuple",
				attr.SlogEvent("mcp_attribution_tuple_store_failed"),
				attr.SlogError(err),
				attr.SlogGenAIConversationID(sessionID),
			)
		}
	}
}
