package risk_analysis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
)

func (a *AnalyzeBatch) scanCustomRules(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage, customRuleIDs []string) ([][]scanners.Finding, error) {
	a.publishCustomRulesScanRequests(ctx, args, requestID, messages, customRuleIDs)

	// ScanBatch loads the rules from the database once for the whole batch and
	// evaluates them against each message through its caching CEL evaluator,
	// keeping DB work constant in the number of messages. It re-loads and
	// compiles the rules internally, so we hand it the resolved ids directly.
	scanMessages := make([]customruleanalyzer.ScanMessage, 0, len(messages))
	for _, msg := range messages {
		view := batchMessageView(msg)
		toolCalls := make([]customruleanalyzer.ScanToolCall, 0, len(view.Tools))
		for _, t := range view.Tools {
			toolCalls = append(toolCalls, customruleanalyzer.ScanToolCall{Name: t.Name, Arguments: t.Arguments})
		}

		scanMessages = append(scanMessages, customruleanalyzer.ScanMessage{
			Content:   view.Content,
			Kind:      view.Type,
			ToolCalls: toolCalls,
		})
	}

	out, err := a.customRuleScanner.ScanBatch(ctx, customruleanalyzer.ScanBatchRequest{
		ProjectID:     args.ProjectID,
		CustomRuleIDs: customRuleIDs,
		Messages:      scanMessages,
	})
	if err != nil {
		return [][]scanners.Finding{}, fmt.Errorf("scan with custom rules: %w", err)
	}

	activity.RecordHeartbeat(ctx, SourceCustom)
	return out, nil
}

// publishCustomRulesScanRequests mirrors publishGitleaksScanRequests: it emits a
// CustomRulesAnalysis per message onto the async analyzer topic, carrying the
// full CEL input (content, kind, tool calls) and the selected rule ids so the
// subscriber can re-load and evaluate the same rules against the same message.
func (a *AnalyzeBatch) publishCustomRulesScanRequests(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage, customRuleIDs []string) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, len(messages))
	for i, msg := range messages {
		view := batchMessageView(msg)
		toolCalls := make([]*riskv1.CustomRulesAnalysis_ToolCall, 0, len(view.Tools))
		for _, t := range view.Tools {
			toolCalls = append(toolCalls, riskv1.CustomRulesAnalysis_ToolCall_builder{
				Name:      new(t.Name),
				Arguments: new(t.Arguments),
			}.Build())
		}

		publishResults[i] = a.customRulesPub.Publish(ctx, riskv1.CustomRulesAnalysis_builder{
			RequestId:         new(requestID.String()),
			ChatMessageId:     new(msg.ID.String()),
			ProjectId:         new(args.ProjectID.String()),
			OrganizationId:    &args.OrganizationID,
			RiskPolicyId:      new(args.RiskPolicyID.String()),
			RiskPolicyVersion: &args.PolicyVersion,
			CreatedAt:         &createdAt,

			Content:       new(msg.Content),
			Kind:          new(msg.Type),
			ToolCalls:     toolCalls,
			CustomRuleIds: customRuleIDs,
		}.Build())
	}
	drainPublishAcks(ctx, a.logger, "failed to publish custom rules scan request", publishResults)
}
