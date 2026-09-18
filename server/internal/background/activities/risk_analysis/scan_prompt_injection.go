package risk_analysis

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func (a *AnalyzeBatch) scanPromptInjection(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, contents []string) ([][]scanners.Finding, error) {
	out := make([]scanners.Result, len(messages))
	judgeMessages := make([]judgemessage.Message, len(messages))
	judgeTrajectories := make([]judgemessage.Trajectory, len(messages))
	judgeUserIDs := make([]string, len(messages))
	for i := range messages {
		judgeMessages[i] = batchJudgeMessage(messages[i])
		judgeTrajectories[i] = batchJudgeTrajectory(messages[i])
		judgeUserIDs[i] = messages[i].UserID
	}
	if err := a.publishPromptInjectionScanRequests(ctx, args, messages); err != nil {
		return nil, err
	}

	startedAt := time.Now().UTC()
	results, verdicts, err := a.promptInjectionScanner.ScanBatchWithVerdicts(ctx, contents, args.OrganizationID, args.ProjectID.String(), judgeUserIDs, judgeMessages, judgeTrajectories)
	if err != nil {
		a.logger.WarnContext(ctx, "prompt injection scan failed", attr.SlogError(err))
		return findingsFromResults(out), nil
	}
	activity.RecordHeartbeat(ctx, SourcePromptInjection)
	if results == nil {
		return findingsFromResults(out), nil
	}
	// Surface the full flagged event (body + tool calls) as the Match, replacing
	// the content-only text so tool-request findings — whose content is empty —
	// still show what was flagged in the Risk Events UI.
	for i := range results {
		if len(results[i].Findings) == 0 {
			continue
		}
		ev := judgemessage.Render(judgeMessages[i])
		for j := range results[i].Findings {
			results[i].Findings[j].Match = ev
			results[i].Findings[j].EndPos = len(ev)
		}
	}
	requestID := batchScanRequestID(args, "standard").String()

	for i := range min(len(messages), len(results), len(verdicts)) {
		if !results[i].Completed {
			continue
		}
		provenance := batchRiskProvenance(args, messages[i], inlineBatchExecutionPath, requestID)
		provenance.Model = verdicts[i].Model
		provenance.Provider = verdicts[i].Provider
		if err := a.riskRecorder.Record(ctx, metering.RiskPromptInjection(), provenance, results[i].STokens, startedAt); err != nil {
			a.logger.ErrorContext(ctx, "record prompt injection usage", attr.SlogError(err))
		}
	}
	return findingsFromResults(results), nil
}

func (a *AnalyzeBatch) publishPromptInjectionScanRequests(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(messages))
	requestID := batchScanRequestID(args, "standard")
	for _, msg := range messages {
		provenance := batchRiskProvenance(args, msg, shadowStreamExecutionPath, requestID.String())
		chatMessageID, contentPartID := msg.anchorIDStrings()
		jm := batchJudgeMessage(msg)
		content, priorUserRequest, recentUntrustedContent := msg.Content, msg.PriorUserRequest, msg.RecentUntrustedContent
		if textBytes := len(content) + len(priorUserRequest) + len(recentUntrustedContent) + len(jm.Body) + toolCallBytes(jm.ToolCalls); textBytes > maxPromptInjectionPublishBytes {
			// Pub/Sub refuses a single message above its request limit, and the
			// refusal fails the whole batch on every attempt. Bound each text
			// field so the message publishes; the judge reads a prefix of an
			// oversize message rather than nothing at all.
			a.logger.WarnContext(ctx, "prompt injection scan payload truncated", attr.SlogValueInt(textBytes))
			content = truncateAtRuneBoundary(content, maxPromptInjectionFieldBytes)
			priorUserRequest = truncateAtRuneBoundary(priorUserRequest, maxPromptInjectionFieldBytes)
			recentUntrustedContent = truncateAtRuneBoundary(recentUntrustedContent, maxPromptInjectionFieldBytes)
			jm.Body = truncateAtRuneBoundary(jm.Body, maxPromptInjectionFieldBytes)
			for i := range jm.ToolCalls {
				jm.ToolCalls[i].Arguments = truncateAtRuneBoundary(jm.ToolCalls[i].Arguments, maxPromptInjectionFieldBytes)
			}
		}
		toolCalls := make([]*riskv1.PromptInjectionAnalysis_ToolCall, 0, len(jm.ToolCalls))
		for _, call := range jm.ToolCalls {
			toolCalls = append(toolCalls, riskv1.PromptInjectionAnalysis_ToolCall_builder{
				Name:      &call.ToolName,
				Arguments: &call.Arguments,
			}.Build())
		}

		publishResults = append(publishResults, a.promptInjectionPub.Publish(ctx, riskv1.PromptInjectionAnalysis_builder{
			RequestId:               new(requestID.String()),
			ChatMessageId:           chatMessageID,
			ContentPartId:           contentPartID,
			ProjectId:               new(args.ProjectID.String()),
			OrganizationId:          &args.OrganizationID,
			RiskPolicyId:            new(args.RiskPolicyID.String()),
			RiskPolicyVersion:       &args.PolicyVersion,
			CreatedAt:               &createdAt,
			ChatId:                  nilUUIDString(msg.ChatID),
			ParentChatMessageId:     nilUUIDString(msg.ParentChatMessageID),
			OriginRiskPolicyId:      new(args.RiskPolicyID.String()),
			OriginRiskPolicyVersion: &args.PolicyVersion,
			MessageLinkReason:       &provenance.MessageLinkReason,
			ExecutionPath:           new(shadowStreamExecutionPath),
			ToolCallId:              &provenance.ToolCallID,
			HookSource:              &msg.Source,

			Content:                &content,
			UserId:                 &msg.UserID,
			L1Enabled:              new(true),
			MessageType:            new(jm.Type),
			Body:                   &jm.Body,
			ToolName:               &jm.ToolName,
			ToolCalls:              toolCalls,
			PriorUserRequest:       &priorUserRequest,
			RecentUntrustedContent: &recentUntrustedContent,
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish prompt injection scan requests", publishResults)
}

const (
	// maxPromptInjectionPublishBytes is the text volume above which one scan
	// request is cut down before publishing. Pub/Sub rejects a message over
	// its 10 MiB request limit outright, and the proto carries the message
	// text several times over (content, judge body, surrounding context).
	maxPromptInjectionPublishBytes = 2 << 20
	// maxPromptInjectionFieldBytes bounds each text field of a cut-down
	// request. Five fields at this size stay well under the publish limit.
	maxPromptInjectionFieldBytes = 256 << 10
)

func toolCallBytes(calls []judgemessage.ToolCall) int {
	n := 0
	for _, call := range calls {
		n += len(call.ToolName) + len(call.Arguments)
	}
	return n
}
