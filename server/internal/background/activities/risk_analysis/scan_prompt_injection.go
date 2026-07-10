package risk_analysis

import (
	"context"

	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func (a *AnalyzeBatch) scanPromptInjection(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, contents []string) [][]scanners.Finding {
	out := make([][]scanners.Finding, len(messages))
	l1Enabled := a.projectFlagEnabled(ctx, args.OrganizationID, args.ProjectID, feature.FlagPromptInjectionUseClassifier)
	var judgeMessages []judgemessage.Message
	var judgeUserIDs []string
	if l1Enabled {
		judgeMessages = make([]judgemessage.Message, len(messages))
		judgeUserIDs = make([]string, len(messages))
		for i := range messages {
			judgeMessages[i] = batchJudgeMessage(messages[i])
			judgeUserIDs[i] = messages[i].UserID
		}
	}

	results, err := a.promptInjectionScanner.ScanBatch(ctx, contents, args.OrganizationID, args.ProjectID.String(), judgeUserIDs, judgeMessages, l1Enabled)
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
