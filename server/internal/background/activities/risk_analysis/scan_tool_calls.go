package risk_analysis

import (
	"context"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"

	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/scanners/destructivetool"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// MCPProvenanceLookup replays the MCP server identity recorded for a set of
// tool calls. It is the risk_analysis-facing name for
// shadowmcpscan.ProvenanceLookup, kept here because it is part of the
// NewAnalyzeBatch constructor signature.
type MCPProvenanceLookup interface {
	LookupMCPProvenanceByToolCallID(ctx context.Context, projectID uuid.UUID, toolCallIDs []string, since time.Time) (map[string]telemetryrepo.MCPProvenance, error)
}

func (a *AnalyzeBatch) scanShadowMCP(ctx context.Context, orgID string, projectID uuid.UUID, policyID uuid.UUID, messages []batchMessage) [][]scanners.Finding {
	scanMessages := make([]shadowmcpscan.Message, len(messages))
	for i, msg := range messages {
		msgCalls := make([]shadowmcpscan.ToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			msgCalls = append(msgCalls, shadowmcpscan.ToolCall{
				ID:        call.ID,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
				CreatedAt: msg.CreatedAt,
				Sender:    msg.Source,
			})
		}
		scanMessages[i] = shadowmcpscan.Message{
			UserID:    msg.UserID,
			ToolCalls: msgCalls,
		}
	}
	return a.shadowMCPScanner.Scan(ctx, orgID, projectID, policyID, scanMessages)
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

func (a *AnalyzeBatch) scanDestructiveCLICommands(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage) ([][]scanners.Finding, error) {
	results := make([]scanners.Result, len(messages))
	startedAt := time.Now().UTC()
	for i, msg := range messages {
		calls := make([]clidestructive.ToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			calls = append(calls, clidestructive.ToolCall{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			})
		}
		results[i] = a.cliDestructiveScanner.Scan(ctx, calls)
	}
	a.recordBatchResults(ctx, metering.RiskCLIDestructive(), args, messages, results, startedAt)
	return findingsFromResults(results), nil
}

func (a *AnalyzeBatch) publishToolCallScanRequests(
	ctx context.Context,
	args AnalyzeBatchArgs,
	messages []batchMessage,
	sources sourceSet,
	masks CategoryScopeMasks,
	legacyDestructive bool,
	legacyCLI bool,
) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	results := make([]gcp.PublishResult, 0, len(messages))
	for i, msg := range messages {
		if len(msg.ToolCalls) == 0 {
			continue
		}

		var requestSources []string
		if sources.Has(shadowmcpscan.Source) && masks.AdmitsAny(i, sourceCategories[shadowmcpscan.Source]) {
			requestSources = append(requestSources, shadowmcpscan.Source)
		}
		if legacyDestructive && sources.Has(destructivetool.Source) && masks.AdmitsAny(i, sourceCategories[destructivetool.Source]) {
			requestSources = append(requestSources, destructivetool.Source)
		}
		if legacyCLI && sources.Has(clidestructive.Source) && masks.AdmitsAny(i, sourceCategories[clidestructive.Source]) {
			requestSources = append(requestSources, clidestructive.Source)
		}
		if len(requestSources) == 0 {
			continue
		}

		toolCalls := make([]*riskv1.ToolCallAnalysis_ToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			callCreatedAt := msg.CreatedAt.Format(time.RFC3339Nano)
			toolCalls = append(toolCalls, riskv1.ToolCallAnalysis_ToolCall_builder{
				Id:        &call.ID,
				Name:      &call.Function.Name,
				Arguments: &call.Function.Arguments,
				CreatedAt: &callCreatedAt,
				Sender:    &msg.Source,
			}.Build())
		}

		requestID := messageScanRequestID(args, "standard", msg)
		provenance := batchRiskProvenance(args, msg, shadowStreamExecutionPath, requestID)
		chatMessageID, contentPartID := msg.anchorIDStrings()
		results = append(results, a.toolCallPub.Publish(ctx, riskv1.ToolCallAnalysis_builder{
			RequestId:               &requestID,
			ChatMessageId:           chatMessageID,
			ContentPartId:           contentPartID,
			ProjectId:               new(args.ProjectID.String()),
			OrganizationId:          &args.OrganizationID,
			RiskPolicyId:            new(args.RiskPolicyID.String()),
			RiskPolicyVersion:       &args.PolicyVersion,
			CreatedAt:               &createdAt,
			ToolCalls:               toolCalls,
			Sources:                 requestSources,
			ChatId:                  nilUUIDString(msg.ChatID),
			ParentChatMessageId:     nilUUIDString(msg.ParentChatMessageID),
			OriginRiskPolicyId:      new(args.RiskPolicyID.String()),
			OriginRiskPolicyVersion: &args.PolicyVersion,
			MessageLinkReason:       &provenance.MessageLinkReason,
			ExecutionPath:           new(shadowStreamExecutionPath),
			ToolCallId:              &provenance.ToolCallID,
			ToolName:                &provenance.ToolName,
			HookSource:              &msg.Source,
			UserId:                  &msg.UserID,
			MessageType:             &msg.Type,
			PolicyLinkReason:        &provenance.PolicyLinkReason,
			ExternalConversationId:  &provenance.ExternalConversationID,
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish tool call scan requests", results)
}
