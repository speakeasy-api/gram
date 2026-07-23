package risk_analysis

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func (a *AnalyzeBatch) scanPromptInjection(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage, contents []string) [][]scanners.Finding {
	out := make([][]scanners.Finding, len(messages))
	judgeMessages := make([]judgemessage.Message, len(messages))
	judgeTrajectories := make([]judgemessage.Trajectory, len(messages))
	judgeUserIDs := make([]string, len(messages))
	for i := range messages {
		judgeMessages[i] = batchJudgeMessage(messages[i])
		judgeTrajectories[i] = batchJudgeTrajectory(messages[i])
		judgeUserIDs[i] = messages[i].UserID
	}
	a.publishPromptInjectionScanRequests(ctx, args, requestID, messages)

	results, err := a.promptInjectionScanner.ScanBatch(ctx, contents, args.OrganizationID, args.ProjectID.String(), judgeUserIDs, judgeMessages, judgeTrajectories)
	if err != nil {
		a.logger.WarnContext(ctx, "prompt injection scan failed", attr.SlogError(err))
		return out
	}
	activity.RecordHeartbeat(ctx, SourcePromptInjection)
	if results == nil {
		return out
	}
	// Surface the full flagged event (body + tool calls) as the Match, replacing
	// the content-only text so tool-request findings — whose content is empty —
	// still show what was flagged in the Risk Events UI.
	for i := range results {
		if len(results[i]) == 0 {
			continue
		}
		ev := judgemessage.Render(judgeMessages[i])
		for j := range results[i] {
			results[i][j].Match = ev
			results[i][j].EndPos = len(ev)
		}
	}
	return results
}

func (a *AnalyzeBatch) publishPromptInjectionScanRequests(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(messages))
	for _, msg := range messages {
		jm := batchJudgeMessage(msg)
		toolCalls := make([]*riskv1.PromptInjectionAnalysis_ToolCall, 0, len(jm.ToolCalls))
		for _, call := range jm.ToolCalls {
			toolCalls = append(toolCalls, riskv1.PromptInjectionAnalysis_ToolCall_builder{
				Name:      &call.ToolName,
				Arguments: &call.Arguments,
			}.Build())
		}

		publishResults = append(publishResults, a.promptInjectionPub.Publish(ctx, riskv1.PromptInjectionAnalysis_builder{
			RequestId:         new(requestID.String()),
			ChatMessageId:     new(msg.ID.String()),
			ProjectId:         new(args.ProjectID.String()),
			OrganizationId:    &args.OrganizationID,
			RiskPolicyId:      new(args.RiskPolicyID.String()),
			RiskPolicyVersion: &args.PolicyVersion,
			CreatedAt:         &createdAt,

			Content:                new(msg.Content),
			UserId:                 &msg.UserID,
			L1Enabled:              new(true),
			MessageType:            new(jm.Type),
			Body:                   &jm.Body,
			ToolName:               &jm.ToolName,
			ToolCalls:              toolCalls,
			PriorUserRequest:       &msg.PriorUserRequest,
			RecentUntrustedContent: &msg.RecentUntrustedContent,
		}.Build()))
	}
	drainPublishAcks(ctx, a.logger, "failed to publish prompt injection scan request", publishResults)
}
