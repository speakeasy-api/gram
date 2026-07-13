package hooks

// Transcript-derived MCP attribution intake. Claude redacts user-configured
// MCP server/tool names to "custom" on its OTEL api_request telemetry, but
// records the real names in the local session transcript. The Claude plugin's
// Stop/SubagentStop hooks extract (request_id, mcp_server, mcp_tool) tuples
// from the transcript and ship them on the unified ingest payload
// (data.mcp_attribution). This file stores those tuples in Redis, keyed per
// request id, and kicks the staged-telemetry promotion workflow that joins
// them against the rows parked in telemetry_logs_staging (see
// server/internal/hooks/otel.go for the staging fork and
// server/internal/background/promote_staged_telemetry.go for promotion).

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// captureMCPAttribution stores every attribution tuple the payload carries
// and triggers promotion for the session. Failures are non-fatal: a missed
// tuple only means the affected staged rows promote verbatim (still "custom")
// after the timeout — today's behavior.
func (s *Service) captureMCPAttribution(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext) {
	if payload.Data == nil || len(payload.Data.McpAttribution) == 0 || authCtx.ProjectID == nil {
		return
	}

	sessionID := canonicalSessionID(payload)
	stored := 0
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
		if err := s.cache.Set(ctx, telemetry.MCPAttributionTupleKey(requestID), tuple, telemetry.MCPAttributionTupleTTL); err != nil {
			s.logger.WarnContext(ctx, "failed to store MCP attribution tuple",
				attr.SlogEvent("mcp_attribution_tuple_store_failed"),
				attr.SlogError(err),
				attr.SlogGenAIConversationID(sessionID),
			)
			continue
		}
		stored++
	}
	if stored == 0 || sessionID == "" {
		return
	}

	s.schedulePromoteStagedTelemetry(ctx, *authCtx.ProjectID, sessionID)
}

// schedulePromoteStagedTelemetry starts (or joins) the per-session promotion
// workflow. The workflow ID serializes promotion per session; a start that
// finds a run already in flight is fine — that run, a later trigger, or the
// sweep picks the work up.
func (s *Service) schedulePromoteStagedTelemetry(ctx context.Context, projectID uuid.UUID, sessionID string) {
	// The hooks test harness runs without Temporal; the sweep schedule covers
	// promotion when no trigger fires.
	if s.temporalEnv == nil {
		return
	}
	workflowCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, err := background.ExecutePromoteStagedTelemetryWorkflow(workflowCtx, s.temporalEnv, background.PromoteStagedTelemetryParams{
		ProjectID: projectID,
		SessionID: sessionID,
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to schedule staged telemetry promotion",
			attr.SlogError(err),
			attr.SlogGenAIConversationID(sessionID),
			attr.SlogProjectID(projectID.String()),
		)
	}
}
