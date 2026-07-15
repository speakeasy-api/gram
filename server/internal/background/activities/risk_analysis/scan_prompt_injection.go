package risk_analysis

import (
	"context"

	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func (a *AnalyzeBatch) scanPromptInjection(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, contents []string) [][]scanners.Finding {
	out := make([][]scanners.Finding, len(messages))
	judgeMessages := make([]judgemessage.Message, len(messages))
	judgeUserIDs := make([]string, len(messages))
	for i := range messages {
		judgeMessages[i] = batchJudgeMessage(messages[i])
		judgeUserIDs[i] = messages[i].UserID
	}

	results, err := a.promptInjectionScanner.ScanBatch(ctx, contents, args.OrganizationID, args.ProjectID.String(), judgeUserIDs, judgeMessages)
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
