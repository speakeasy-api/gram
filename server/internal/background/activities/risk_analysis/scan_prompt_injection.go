package risk_analysis

import (
	"context"

	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

func (a *AnalyzeBatch) scanPromptInjection(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, contents []string) [][]Finding {
	out := make([][]Finding, len(messages))
	l1Enabled := a.projectFlagEnabled(ctx, args.OrganizationID, args.ProjectID, feature.FlagPromptInjectionUseClassifier)
	var judgeMessages []JudgeMessage
	if l1Enabled {
		judgeMessages = make([]JudgeMessage, len(messages))
		for i := range messages {
			judgeMessages[i] = batchJudgeMessage(messages[i])
		}
	}

	results, err := a.piScanner.ScanBatch(ctx, contents, args.OrganizationID, args.ProjectID.String(), judgeMessages, l1Enabled)
	if err != nil {
		a.logger.WarnContext(ctx, "prompt injection scan failed", attr.SlogError(err))
		return out
	}
	activity.RecordHeartbeat(ctx, SourcePromptInjection)
	if results == nil {
		return out
	}
	return results
}
